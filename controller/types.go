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

type springConfig struct {
	Spring spring `json:"spring"`
}

type spring struct {
	Cloud cloud `json:"cloud"`
}

type cloud struct {
	Gateway gateway `json:"gateway"`
}

type gateway struct {
	Routes []route `json:"routes"`
}

type route struct {
	ID         string   `json:"id,omitempty"`
	URI        string   `json:"uri,omitempty"`
	Predicates []string `json:"predicates,omitempty"`
	Filters    []string `json:"filters,omitempty"`
}
