package controller

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	replicationv1alpha1 "github.com/RedHatInsights/pg-replication-operator/api/v1alpha1"
	"github.com/RedHatInsights/pg-replication-operator/internal/replication"
)

func generateDbCredentials(database string) replication.DatabaseCredentials {
	return replication.DatabaseCredentials{
		Host:          pgCredentials.Host,
		Port:          pgCredentials.Port,
		User:          database + "_user",
		Password:      database + "_password",
		AdminPassword: pgCredentials.AdminPassword,
		AdminUser:     pgCredentials.AdminUser,
		DatabaseName:  database + "_db",
	}
}

func runReconcile(ctx context.Context, namespace types.NamespacedName) (controllerruntime.Result, error) {
	controllerReconciler := &LogicalReplicationReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
	}

	result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: namespace,
	})
	return result, err
}

func generateDbSecret(ctx context.Context, nn types.NamespacedName, database string) *corev1.Secret {
	secret := &corev1.Secret{}

	err := k8sClient.Get(ctx, nn, secret)
	if err != nil && errors.IsNotFound(err) {
		credentials := generateDbCredentials(database)
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
			Data: map[string][]byte{
				"db.host":           []byte(credentials.Host),
				"db.port":           []byte(credentials.Port),
				"db.user":           []byte(credentials.User),
				"db.password":       []byte(credentials.Password),
				"db.admin_password": []byte(credentials.AdminPassword),
				"db.admin_user":     []byte(credentials.AdminUser),
				"db.name":           []byte(credentials.DatabaseName),
			},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		return secret
	}

	Expect(err).ShouldNot(HaveOccurred())
	return secret
}

var _ = Describe("LogicalReplication Controller", func() {
	var (
		publisherDB  *sql.DB
		subscriberDB *sql.DB
	)

	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const publishingSecretName = "publishing-database"
		const subscribingSecretname = "subscribing-database"
		const publicationName = "publication_v1"

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
							Name:       publicationName,
							SecretName: publishingSecretName,
						},
						Subscription: replicationv1alpha1.SubscriptionSpec{
							SecretName: subscribingSecretname,
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}

			By("connecting to test databases")
			publisherDB, err = replication.DBConnect(generateDbCredentials("publisher"))
			Expect(err).NotTo(HaveOccurred())
			subscriberDB, err = replication.DBConnect(generateDbCredentials("subscriber"))
			Expect(err).NotTo(HaveOccurred())
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

		It("should reconcile if table has different columns", func() {
			By("remove table")
			_, err := subscriberDB.Exec("ALTER TABLE published_data.people ADD extra_column timestamp")
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling the created resource")
			_, err = runReconcile(ctx, typeNamespacedName)

			Expect(err).NotTo(HaveOccurred())
		})

		It("should reconcile if table does not exist", func() {
			By("remove table")
			_, err := subscriberDB.Exec("DROP TABLE published_data.people")
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling the created resource")
			_, err = runReconcile(ctx, typeNamespacedName)

			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail when publication does not exist", func() {
			By("remove publication")
			_, err := publisherDB.Exec("DROP PUBLICATION " + publicationName)
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling the created resource")
			result, err := runReconcile(ctx, typeNamespacedName)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())
		})

		It("should fail when can't connect to subscriber db", func() {
			By("disable subscriber user")
			_, err := subscriberDB.Exec("ALTER USER subscriber_user PASSWORD 'changed'")
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling the created resource")
			result, err := runReconcile(ctx, typeNamespacedName)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())

			_, err = subscriberDB.Exec("ALTER USER subscriber_user PASSWORD 'subscriber_password'")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail when can't connect to publisher db", func() {
			By("disable publisher user")
			_, err := publisherDB.Exec("ALTER USER publisher_user PASSWORD 'changed'")
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling the created resource")
			result, err := runReconcile(ctx, typeNamespacedName)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())

			_, err = publisherDB.Exec("ALTER USER publisher_user PASSWORD 'publisher_password'")
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
