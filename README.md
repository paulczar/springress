# Spring Cloud Gateway Ingress Controller (Springress)

Springress is a minimal implementation of a Kubernetes Ingress Controller using Spring Cloud Gateway.

Currently it only supports very basic routing and the ingress type annotation must be set to `spring`.

There are two components to the project, the Spring Cloud Gateway application and a controller written in Go that converts ingress resources into a set of routes.

## Components

### Springress Controller

Written in Go it watches `ingress` resources and modifies a ConfigMap `springress` which contains a Spring Cloud Gateway config listing all of the routes for the gateway service to route for.

### Springress Gateway

Written in Spring and taking advantage of Spring Cloud Kubernetes it automatically loads the config provided in the `springress` ConfigMap and routes for them.

### Running

You'll find a Helm chart in `helm/springress` which you can use if you're so inclined, otherwise you can use the manifests found in `examples`.

Deploy Springress:

```bash
kubectl apply -f examples/springress.yaml
```

Check its running:

> Note: the gateway can take up to 60 seconds to start

```bash
kubectl get pods
NAME                                    READY   STATUS    RESTARTS   AGE
springress-controller-75c75c654-dh9d6   1/1     Running   0          2m47s
springress-gateway-676c4657f4-gp25h     1/1     Running   0          2m47s
```

In a second terminal start a kubectl port-forard:

```bash
kubectl port-forward deployment/springress-gateway 9000:9000
```

Check the Gateway is responding:

```bash
$ curl localhost:9000
{"timestamp":1571508525936,"path":"/","status":404,"error":"Not Found","message":"No matching handler","requestId":"f3a3e68e"}

$ curl localhost:9000/actuator/health
{"status":"UP"}
```

Apply the foo/bar deployments and ingress:

```bash
kubectl apply -f examples/foo-bar-ingress.yaml
```

At this stage you can test that the controller picked up the Ingress resource:

```bash
$ kubectl logs --tail 10 deployment/springress-controller
W1019 18:05:04.700643       6 client_config.go:541] Neither --kubeconfig nor --master was specified.  Using the inClusterConfig.  This might not work.
I1019 18:05:04.705436       6 main.go:187] Starting Ingress controller
2019/10/19 18:05:04 annotation 'kubernetes.io/ingress.class' not set for spring
Sync/Add/Update for Ingress foo-bar
Created configmap "springress".
```

Validate that the configmap was created properly:

```bash
$ kubectl get configmap springress -o yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: springress
data:
  application.yaml: |
    spring:
      cloud:
        gateway:
          routes:
          - id: 2d4910e2-9fe6-4ea7-a0d1-56cf84e2fbec
            predicates:
            - Host=foo.com
            uri: http://foo:8080
          - id: b74df249-5e48-4705-a30b-b8915d7c9b45
            predicates:
            - Host=bar.com
            uri: http://bar:8080
```

Test the gateway is routing your applications correctly:

```bash
$ curl -H "Host: foo.com" localhost:9000
HELLO FOO
$ curl -H "Host: bar.com" localhost:9000
HELLO BAR
```
