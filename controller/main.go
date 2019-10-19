/*
Copyright 2019 Paul Czarkowski <pczarkowski@pivotal.io>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/ghodss/yaml"
	"github.com/google/uuid"
	v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

// import "github.com/davecgh/go-spew/spew"
const annotation = "kubernetes.io/ingress.class"

type Controller struct {
	indexer   cache.Indexer
	queue     workqueue.RateLimitingInterface
	informer  cache.Controller
	clientset kubernetes.Clientset
}

func NewController(queue workqueue.RateLimitingInterface, indexer cache.Indexer, informer cache.Controller, clientset kubernetes.Clientset) *Controller {
	return &Controller{
		informer:  informer,
		indexer:   indexer,
		queue:     queue,
		clientset: clientset,
	}
}

func (c *Controller) processNextItem() bool {
	// Wait until there is a new item in the working queue
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	// Tell the queue that we are done with processing this key. This unblocks the key for other workers
	// This allows safe parallel processing because two Ingress with the same key are never processed in
	// parallel.
	defer c.queue.Done(key)

	// Invoke the method containing the business logic
	err := c.syncToConfigMap(key.(string))
	// Handle the error if something went wrong during the execution of the business logic
	c.handleErr(err, key)
	return true
}

// syncToConfigMap is the business logic of the controller. It takes ingress resources and
// builds them into a spring cloud gateway friendly configmap
func (c *Controller) syncToConfigMap(key string) error {
	var springConfig springConfig
	var routes []route
	obj, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		klog.Errorf("Fetching object with key %s from store failed with %v", key, err)
		return err
	}

	if !exists {
		// What to do if a ingress has been deleted
		fmt.Printf("Ingress %s does not exist anymore\n", key)
	} else {
		// ensure annotation set for spring
		a := obj.(*extensions.Ingress).GetAnnotations()
		value, ok := a[annotation]
		if !ok {
			log.Printf("Required '%s' annotation missing; skipping ingress", annotation)
			return nil
		} else if value != "spring" {
			log.Printf("annotation '%s' not set for spring", annotation)
			return nil
		}
		// Note that you also have to check the uid if you have a local controlled resource, which
		// is dependent on the actual instance, to detect that a Ingress was recreated with the same name
		fmt.Printf("Sync/Add/Update for Ingress %s\n", obj.(*extensions.Ingress).GetName())
		rules := obj.(*extensions.Ingress).Spec.Rules
		for _, rule := range rules {
			host := rule.Host
			paths := rule.HTTP.Paths
			for _, path := range paths {
				newRoute := route{
					ID: uuid.New().String(),
				}
				newRoute.URI = "http://" + path.Backend.ServiceName + ":" + path.Backend.ServicePort.String()
				if path.Path != "" {
					newRoute.Predicates = append(newRoute.Predicates, "Path="+path.Path)
				}
				if host != "" {
					newRoute.Predicates = append(newRoute.Predicates, "Host="+host)
				}
				routes = append(routes, newRoute)
			}
		}
		springConfig.Spring.Cloud.Gateway.Routes = routes
		springConfigYAML, _ := yaml.Marshal(springConfig)
		//fmt.Printf("%s", string(springConfigYAML))

		var data = map[string]string{}
		data["application.yaml"] = string(springConfigYAML)
		configmap := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "springress",
			},
			Data: data,
		}
		configMapsClient := c.clientset.CoreV1().ConfigMaps(v1.NamespaceDefault)
		result, err := configMapsClient.Update(configmap)
		if err != nil {
			if errors.IsNotFound(err) {
				result, err := configMapsClient.Create(configmap)
				if err != nil {
					panic(err)
				}
				fmt.Printf("Created configmap %q.\n", result.GetObjectMeta().GetName())
			} else {
				panic(err)
			}
		} else {
			fmt.Printf("Updated configmap %q.\n", result.GetObjectMeta().GetName())
		}
	}
	return nil
}

// handleErr checks if an error happened and makes sure we will retry later.
func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		// Forget about the #AddRateLimited history of the key on every successful synchronization.
		// This ensures that future processing of updates for this key is not delayed because of
		// an outdated error history.
		c.queue.Forget(key)
		return
	}

	// This controller retries 5 times if something goes wrong. After that, it stops trying.
	if c.queue.NumRequeues(key) < 5 {
		klog.Infof("Error syncing Ingress %v: %v", key, err)

		// Re-enqueue the key rate limited. Based on the rate limiter on the
		// queue and the re-enqueue history, the key will be processed later again.
		c.queue.AddRateLimited(key)
		return
	}

	c.queue.Forget(key)
	// Report to an external entity that, even after several retries, we could not successfully process this key
	runtime.HandleError(err)
	klog.Infof("Dropping ingress %q out of the queue: %v", key, err)
}

func (c *Controller) Run(threadiness int, stopCh chan struct{}) {
	defer runtime.HandleCrash()

	// Let the workers stop when we are done
	defer c.queue.ShutDown()
	klog.Info("Starting Ingress controller")

	go c.informer.Run(stopCh)

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
	klog.Info("Stopping Ingress controller")
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

func main() {
	var kubeconfig string
	var master string

	flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	flag.StringVar(&master, "master", "", "master url")
	flag.Parse()

	// creates the connection
	config, err := clientcmd.BuildConfigFromFlags(master, kubeconfig)
	if err != nil {
		klog.Fatal(err)
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatal(err)
	}

	// create the ingress watcher
	ingressListWatcher := cache.NewListWatchFromClient(clientset.ExtensionsV1beta1().RESTClient(),
		"ingresses",
		v1.NamespaceAll,
		fields.Everything())

	// create the workqueue
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	// Bind the workqueue to a cache with the help of an informer. This way we make sure that
	// whenever the cache is updated, the ingress key is added to the workqueue.
	// Note that when we finally process the item from the workqueue, we might see a newer version
	// of the Ingress than the version which was responsible for triggering the update.
	indexer, informer := cache.NewIndexerInformer(ingressListWatcher, &extensions.Ingress{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			// IndexerInformer uses a delta queue, therefore for deletes we have to use this
			// key function.
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
	}, cache.Indexers{})

	controller := NewController(queue, indexer, informer, *clientset)

	// Now let's start the controller
	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(1, stop)

	// Wait forever
	select {}
}
