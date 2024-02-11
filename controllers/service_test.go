package controllers_test

import (
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/Tanemahuta/avahi-lb/controllers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	timeout = time.Second * 5
	poll    = time.Millisecond * 100
)

var _ = Describe("Service controller", Serial, func() {
	Context("generated hostname", RunTest(""))
	Context("explicit hostname", RunTest("_explicit"))
	Context("no IP address", RunTest("_no_ip"))
	Context("no annotations", RunTest("_no_annotations"))
	Context("no annotation", RunTest("_no_annotation"))
})

func RunTest(suffix string) func() {
	noDeployment := strings.HasPrefix(suffix, "_no")
	return func() {
		var (
			service       *corev1.Service
			status        corev1.ServiceStatus
			expDeployment *appsv1.Deployment
			sut           *controllers.Service
		)
		BeforeEach(func() {
			By("Reading the source service")
			service = ReadResource[*corev1.Service]("../config/samples/service" + suffix + ".yaml")
			service.Status.DeepCopyInto(&status)
			service.Status = corev1.ServiceStatus{}
			By("Reading the expected Deployment")
			if noDeployment {
				expDeployment = &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Namespace: service.Namespace, Name: "avahi-" + service.Name},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"a": "b"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "b"}},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{Name: "test", Image: "busybox:latest"}},
							},
						},
					},
				}
			} else {
				expDeployment = ReadResource[*appsv1.Deployment]("testdata/expected/deployment" + suffix + ".yaml")
			}
			By("Creating the service")
			Expect(k8sClient.Create(ctx, service)).NotTo(HaveOccurred())
			sut = &controllers.Service{
				HostnameSuffix: "my-cluster.local",
				Client:         k8sClient,
			}
			By("Waiting for the service to be created")
			Eventually(func() error {
				found := &corev1.Service{}
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(service), found)
			}, timeout, poll).Should(Succeed())
		})
		AfterEach(func() {
			By("Deleting the Namespace to perform the tests")
			_ = k8sClient.Delete(ctx, service)
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), expDeployment)
			_ = k8sClient.Delete(ctx, expDeployment)
		})
		It("should skip the deployment when reconciling", func() {
			By("Reconciling")
			_, err := sut.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(service)})
			actDeployment := &appsv1.Deployment{}
			Expect(err).NotTo(HaveOccurred())
			By("Waiting for the deployment to appear")
			<-time.After(timeout / 2)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), actDeployment)).To(
				WithTransform(k8serrors.IsNotFound, BeTrue()),
			)
		})
		When("status contains IP", func() {
			BeforeEach(func() {
				serviceCopy := &corev1.Service{}
				service.DeepCopyInto(serviceCopy)
				patch := client.MergeFrom(serviceCopy)
				service.Status = status
				Expect(k8sClient.Status().Patch(ctx, service, patch)).NotTo(HaveOccurred())
			})
			if noDeployment {
				It("should skip the deployment when reconciling", func() {
					By("Reconciling")
					_, err := sut.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(service)})
					actDeployment := &appsv1.Deployment{}
					Expect(err).NotTo(HaveOccurred())
					By("Waiting for the deployment to appear")
					<-time.After(timeout / 2)
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), actDeployment)).To(
						WithTransform(k8serrors.IsNotFound, BeTrue()),
					)
				})
				When("Deployment exists", func() {
					BeforeEach(func() {
						actDeployment := &appsv1.Deployment{}
						expDeployment.DeepCopyInto(actDeployment)
						Expect(k8sClient.Create(ctx, actDeployment)).NotTo(HaveOccurred())
					})
					It("should remove the deployment", func() {
						By("Reconciling")
						_, err := sut.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(service)})
						Expect(err).NotTo(HaveOccurred())
						actDeployment := &appsv1.Deployment{}
						Eventually(func() error {
							return k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), actDeployment)
						}, timeout, poll).ShouldNot(Succeed())
					})
				})
				return
			}
			It("should create the deployment when reconciling", func() {
				By("Reconciling")
				_, err := sut.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(service)})
				actDeployment := &appsv1.Deployment{}
				Expect(err).NotTo(HaveOccurred())
				By("Waiting for the deployment to appear")
				Eventually(func() error {
					return k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), actDeployment)
				}, timeout, poll).Should(Succeed())
				copyMeta(expDeployment, service, actDeployment)
				data, _ := json.Marshal(expDeployment)
				Expect(json.Marshal(actDeployment)).To(MatchJSON(data))
			})
			When("service is modified", func() {
				BeforeEach(func() {
					service.Status.LoadBalancer.Ingress[0].IP = "10.0.0.2"
					Expect(k8sClient.Status().Update(ctx, service)).NotTo(HaveOccurred())
					actDeployment := &appsv1.Deployment{}
					expDeployment.DeepCopyInto(actDeployment)
					actDeployment.OwnerReferences[0].UID = service.UID
					Expect(k8sClient.Create(ctx, actDeployment)).NotTo(HaveOccurred())
					Eventually(func() string {
						actService := &corev1.Service{}
						_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(service), actService)
						return actService.Status.LoadBalancer.Ingress[0].IP
					}, timeout, poll).Should(Equal("10.0.0.2"))
				})
				It("should patch the deployment when reconciling", func() {
					By("Reconciling")
					_, err := sut.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(service)})
					Expect(err).NotTo(HaveOccurred())
					By("Waiting for the deployment to appear")
					Eventually(func() string {
						actDeployment := &appsv1.Deployment{}
						_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), actDeployment)
						return actDeployment.Spec.Template.Spec.Containers[0].Args[1]
					}, timeout, poll).Should(Equal(service.Status.LoadBalancer.Ingress[0].IP))
				})
			})
			When("service is removed", func() {
				BeforeEach(func() {
					Expect(k8sClient.Delete(ctx, service)).NotTo(HaveOccurred())
					Eventually(func() error {
						actService := &corev1.Service{}
						return k8sClient.Get(ctx, client.ObjectKeyFromObject(service), actService)
					}, timeout, poll).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))
				})
				It("should remove the deployment", func() {
					By("Reconciling")
					_, err := sut.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(service)})
					Expect(err).NotTo(HaveOccurred())
					actDeployment := &appsv1.Deployment{}
					Eventually(func() error {
						return k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), actDeployment)
					}, timeout, poll).ShouldNot(Succeed())
				})
			})
		})
	}
}

func copyMeta(expDeployment *appsv1.Deployment, service *corev1.Service, actDeployment *appsv1.Deployment) {
	expDeployment.OwnerReferences[0].UID = service.UID
	actDeployment.ManagedFields = nil
	expDeployment.APIVersion = actDeployment.APIVersion
	expDeployment.Kind = actDeployment.Kind
	expDeployment.UID = actDeployment.UID
	expDeployment.CreationTimestamp = actDeployment.CreationTimestamp
	expDeployment.ResourceVersion = actDeployment.ResourceVersion
	expDeployment.Generation = actDeployment.Generation
	expDeployment.Spec.Template.Spec.RestartPolicy =
		actDeployment.Spec.Template.Spec.RestartPolicy
	expDeployment.Spec.Template.Spec.SecurityContext =
		actDeployment.Spec.Template.Spec.SecurityContext
	expDeployment.Spec.Template.Spec.SchedulerName =
		actDeployment.Spec.Template.Spec.SchedulerName
	expDeployment.Spec.Template.Spec.TerminationGracePeriodSeconds =
		actDeployment.Spec.Template.Spec.TerminationGracePeriodSeconds
	expDeployment.Spec.Template.Spec.DNSPolicy =
		actDeployment.Spec.Template.Spec.DNSPolicy
	expDeployment.Spec.Template.Spec.Containers[0].TerminationMessagePolicy =
		actDeployment.Spec.Template.Spec.Containers[0].TerminationMessagePolicy
	expDeployment.Spec.Template.Spec.Containers[0].TerminationMessagePath =
		actDeployment.Spec.Template.Spec.Containers[0].TerminationMessagePath
	expDeployment.Spec.Strategy = actDeployment.Spec.Strategy
	expDeployment.Spec.RevisionHistoryLimit = actDeployment.Spec.RevisionHistoryLimit
	expDeployment.Spec.ProgressDeadlineSeconds = actDeployment.Spec.ProgressDeadlineSeconds
}

func ReadResource[O client.Object](filename string) O {
	data, err := os.ReadFile(filename)
	Expect(err).NotTo(HaveOccurred())
	//nolint:errcheck // this is the case.
	result := reflect.New(reflect.TypeOf((*O)(nil)).Elem().Elem()).Interface().(O)
	Expect(yaml.Unmarshal(data, result)).NotTo(HaveOccurred())
	return result
}
