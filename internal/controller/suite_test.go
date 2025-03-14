package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	testcontainers "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	replicationv1alpha1 "github.com/RedHatInsights/pg-replication-operator/api/v1alpha1"
	"github.com/RedHatInsights/pg-replication-operator/internal/replication"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

const postgresImage = "docker.io/library/postgres:16-alpine"

// func setupPgContainer(ctx context.Context) string {
func setupPgContainer(ctx context.Context) (*postgres.PostgresContainer, replication.DatabaseCredentials) {
	os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")

	pgContainer, err := postgres.Run(ctx, postgresImage,
		testcontainers.CustomizeRequest(testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Cmd: []string{"-c", "wal_level=logical", "-c", "listen_addresses=*"},
			},
		}),
		postgres.WithInitScripts(filepath.Join("..", "..", "test", "data", "create_databases.sql")),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	Expect(err).Should(BeNil())

	DeferCleanup(cancel)

	host, err := pgContainer.Host(ctx)
	Expect(err).ToNot(HaveOccurred())
	port, err := pgContainer.MappedPort(ctx, "5432")
	Expect(err).ToNot(HaveOccurred())
	portStr := port.Port()
	pgCredentials := replication.DatabaseCredentials{
		Host:          host,
		Port:          portStr,
		AdminUser:     "postgres",
		AdminPassword: "postgres",
	}

	// we need the same port for postgres both externaly and internaly
	_, _, err = pgContainer.Exec(ctx, []string{"sh", "-c", "nc -lkp" + portStr + " -e nc localhost 5432 &"})
	Expect(err).Should(BeNil())

	// finish subscription
	_, _, err = pgContainer.Exec(ctx, []string{"psql", "-U", "postgres", "-d", "subscriber_db",
		"-c", "ALTER SUBSCRIPTION publication_v1 CONNECTION 'host=localhost port=" + portStr + " user=publisher_user password=publisher_password dbname=publisher_db sslmode=disable';",
		"-c", "ALTER SUBSCRIPTION publication_v1 ENABLE;",
		"-c", "ALTER SUBSCRIPTION publication_v1 REFRESH PUBLICATION;"})
	Expect(err).Should(BeNil())

	return pgContainer, pgCredentials
}

func shutdownPgContainer(ctx context.Context, container *postgres.PostgresContainer) {
	Expect(container.Terminate(ctx)).Should(BeNil())
}

var pgContainer *postgres.PostgresContainer
var pgCredentials replication.DatabaseCredentials

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("1.31.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = replicationv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&LogicalReplicationReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

	By("starting postgresql container")
	pgContainer, pgCredentials = setupPgContainer(ctx)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
	By("tearing down postgres container")
	shutdownPgContainer(context.Background(), pgContainer)
})
