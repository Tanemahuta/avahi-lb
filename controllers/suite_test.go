/*
Copyright 2023.

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

package controllers_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Tanemahuta/avahi-lb/controllers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg              *rest.Config
	k8sClient        client.Client
	testEnv          *envtest.Environment
	namespace        *corev1.Namespace
	publishNamespace *corev1.Namespace
	ctx              context.Context
)

const testAvahiPublishImage = "ghcr.io/tanemahuta/avahi-publish:0.9_rc4-r0-alpine3.24"

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	if len(os.Getenv("KUBEBUILDER_ASSETS")) == 0 {
		cmd := exec.Command("make", "envtestdir")
		cmd.Dir = "../"
		var sb strings.Builder
		cmd.Stdout = &sb
		Expect(cmd.Run()).NotTo(HaveOccurred())
		Expect(os.Setenv("KUBEBUILDER_ASSETS", lastLine(sb.String()))).NotTo(HaveOccurred())
	}

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
	ctx = context.Background()
	By("Creating the Namespace to perform the tests")
	namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"}}
	Expect(k8sClient.Create(ctx, namespace)).NotTo(HaveOccurred())
	publishNamespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "avahi-lb-system"}}
	Expect(k8sClient.Create(ctx, publishNamespace)).NotTo(HaveOccurred())
})

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	for idx := len(lines); idx > 0; idx-- {
		line := lines[idx-1]
		if len(line) > 0 {
			return line
		}
	}
	return ""
}

func SetupReconciler(
	reconciler controllers.AvahiReconciler,
	config *controllers.AvahiConfig,
) controllers.AvahiReconciler {
	skipNameValidation := true
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme.Scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		Client: client.Options{Cache: &client.CacheOptions{DisableFor: []client.Object{
			&corev1.Service{},
			&networkingv1.Ingress{},
			&networkingv1.IngressClass{},
			&appsv1.Deployment{},
		}}},
		Controller: controllerconfig.Controller{SkipNameValidation: &skipNameValidation},
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(reconciler.SetupWithManager(mgr, config)).To(Succeed())
	return reconciler
}

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if k8sClient != nil {
		_ = k8sClient.Delete(ctx, namespace, client.PropagationPolicy(metav1.DeletePropagationForeground))
		_ = k8sClient.Delete(
			ctx,
			publishNamespace,
			client.PropagationPolicy(metav1.DeletePropagationForeground),
		)
	}
	if cfg != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})
