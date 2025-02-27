/*
Copyright 2022 The OpenYurt Authors.

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

package config

import (
	"fmt"
	"path/filepath"
	"strings"

	v1 "k8s.io/api/core/v1"

	fileutil "github.com/openyurtio/openyurt/pkg/util/file"
)

// ControlPlaneConfig has the information that required by node-servant config control-plane operation
type ControlPlaneConfig struct {
	RunMode          string
	KASStaticPodPath string
	KCMStaticPodPath string
}

type Runner interface {
	Do() error
}

func NewControlPlaneRunner(o *ControlPlaneOptions) (Runner, error) {
	switch o.RunMode {
	case "pod":
		return newStaticPodRunner(o.PodManifestsPath)
	default:
		return nil, fmt.Errorf("%s mode is not supported, only static pod mode is implemented", o.RunMode)
	}
}

type staticPodRunner struct {
	kasStaticPodPath string
	kcmStaticPodPath string
}

func newStaticPodRunner(podManifestsPath string) (Runner, error) {
	kasStaticPodPath := filepath.Join(podManifestsPath, "kube-apiserver.yaml")
	kcmStaticPodPath := filepath.Join(podManifestsPath, "kube-controller-manager.yaml")
	if exist, _ := fileutil.FileExists(kasStaticPodPath); !exist {
		return nil, fmt.Errorf("%s file is not exist", kasStaticPodPath)
	}

	if exist, _ := fileutil.FileExists(kcmStaticPodPath); !exist {
		return nil, fmt.Errorf("%s file is not exist", kcmStaticPodPath)
	}

	return &staticPodRunner{
		kasStaticPodPath: kasStaticPodPath,
		kcmStaticPodPath: kcmStaticPodPath,
	}, nil
}

func (spr *staticPodRunner) Do() error {
	var kasPodUpdated bool
	var kcmPodUpdated bool
	// read kube-apiserver static pod
	kasObj, err := fileutil.ReadObjectFromYamlFile(spr.kasStaticPodPath)
	if err != nil {
		return err
	}
	kasPod, ok := kasObj.(*v1.Pod)
	if !ok {
		return fmt.Errorf("manifest file(%s) is not a static pod", spr.kasStaticPodPath)
	}

	// remove --kubelet-preferred-address-types parameter in order to make sure kube-apiserver
	// to use hostname to access nodes on edge node
	for i := range kasPod.Spec.Containers {
		for j := range kasPod.Spec.Containers[i].Command {
			if strings.Contains(kasPod.Spec.Containers[i].Command[j], "kubelet-preferred-address-types=") {
				// remove --kubelet-preferred-address-types parameter setting
				kasPod.Spec.Containers[i].Command = append(kasPod.Spec.Containers[i].Command[:j], kasPod.Spec.Containers[i].Command[j+1:]...)
				kasPodUpdated = true
				break
			}
		}
	}
	// set dnsPolicy to ClusterFirstWithHostNet in order to make sure kube-apiserver
	// will use coredns to resolve hostname. by the way, hostname of edge nodes will be resolved
	// to the service(x-tunnel-server-internal-svc) clusterIP of yurt-tunnel-server
	if kasPod.Spec.DNSPolicy != v1.DNSClusterFirstWithHostNet {
		kasPod.Spec.DNSPolicy = v1.DNSClusterFirstWithHostNet
		kasPodUpdated = true
	}

	// read kube-controller-manager static pod
	kcmObj, err := fileutil.ReadObjectFromYamlFile(spr.kcmStaticPodPath)
	if err != nil {
		return err
	}
	kcmPod, ok := kcmObj.(*v1.Pod)
	if !ok {
		return fmt.Errorf("manifest file(%s) is not a static pod", spr.kcmStaticPodPath)
	}

	// disable NodeLifeCycle controller
	for i := range kcmPod.Spec.Containers {
		for j := range kcmPod.Spec.Containers[i].Command {
			if strings.Contains(kcmPod.Spec.Containers[i].Command[j], "--controllers=") {
				if !strings.Contains(kcmPod.Spec.Containers[i].Command[j], "-nodelifecycle,") {
					// insert -nodelifecycle, after =
					insertPoint := strings.Index(kcmPod.Spec.Containers[i].Command[j], "=") + 1
					kcmPod.Spec.Containers[i].Command[j] = kcmPod.Spec.Containers[i].Command[j][:insertPoint] + "-nodelifecycle," + kcmPod.Spec.Containers[i].Command[j][insertPoint:]
					kcmPodUpdated = true
					break
				}
			}
		}
	}

	// update static pod files
	if kasPodUpdated {
		if err := fileutil.WriteObjectToYamlFile(kasPod, spr.kasStaticPodPath); err != nil {
			return err
		}
	}

	if kcmPodUpdated {
		if err := fileutil.WriteObjectToYamlFile(kcmPod, spr.kcmStaticPodPath); err != nil {
			return err
		}
	}

	return nil
}
