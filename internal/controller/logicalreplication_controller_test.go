package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	replicationv1alpha1 "github.com/RedHatInsights/pg-replication-operator/api/v1alpha1"
)

func generateDbSecret(ctx context.Context, nn types.NamespacedName, database string) *corev1.Secret {
	secret := &corev1.Secret{}

	err := k8sClient.Get(ctx, nn, secret)
	if err != nil && errors.IsNotFound(err) {
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
			Data: map[string][]byte{
				"db.host":           []byte(pgCredentials.Host),
				"db.port":           []byte(pgCredentials.Port),
				"db.user":           []byte(database + "_user"),
				"db.password":       []byte(database + "_password"),
				"db.admin_password": []byte(pgCredentials.AdminPassword),
				"db.admin_user":     []byte(pgCredentials.AdminUser),
				"db.name":           []byte(database + "_db"),
			},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		return secret
	}

	Expect(err).ShouldNot(HaveOccurred())
	return secret
}

var _ = Describe("LogicalReplication Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const publishingSecretName = "publishing-database"
		const subscribingSecretname = "subscribing-database"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		logicalreplication := &replicationv1alpha1.LogicalReplication{}

		BeforeEach(func() {
			By("creating the publication database secret")
			publSecretNN := types.NamespacedName{
				Name:      publishingSecretName,
				Namespace: typeNamespacedName.Namespace,
			}
			generateDbSecret(ctx, publSecretNN, "publisher")

			By("creating the subscribing database secret")
			subSecretNN := types.NamespacedName{
				Name:      subscribingSecretname,
				Namespace: typeNamespacedName.Namespace,
			}
			generateDbSecret(ctx, subSecretNN, "subscriber")

			By("creating the custom resource for the Kind LogicalReplication")
			err := k8sClient.Get(ctx, typeNamespacedName, logicalreplication)
			if err != nil && errors.IsNotFound(err) {
				resource := &replicationv1alpha1.LogicalReplication{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: replicationv1alpha1.LogicalReplicationSpec{
						Publication: replicationv1alpha1.PublicationSpec{
							Name:       "publication_v1",
							SecretName: publishingSecretName,
						},
						Subscription: replicationv1alpha1.SubscriptionSpec{
							SecretName: subscribingSecretname,
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &replicationv1alpha1.LogicalReplication{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance LogicalReplication")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &LogicalReplicationReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
