package controller

import (
	"context"
	"database/sql"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	"github.com/go-viper/mapstructure/v2"

	replicationv1alpha1 "github.com/RedHatInsights/pg-replication-operator/api/v1alpha1"
	"github.com/RedHatInsights/pg-replication-operator/internal/replication"
)

// LogicalReplicationReconciler reconciles a LogicalReplication object
type LogicalReplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=replication.console.redhat.com,resources=logicalreplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=replication.console.redhat.com,resources=logicalreplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=replication.console.redhat.com,resources=logicalreplications/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the LogicalReplication object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *LogicalReplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var _ = log.FromContext(ctx)

	// Get the LogicalReplication object from the API
	lr := &replicationv1alpha1.LogicalReplication{}
	if err := r.Client.Get(ctx, req.NamespacedName, lr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	iteration := NewLogicalReplicationIteration(r.Client, ctx, req)

	err := iteration.Iterate(lr)
	if err != nil {
		statusErr := r.setFailedStatus(ctx, lr, err)
		return ctrl.Result{Requeue: true}, statusErr
	}

	return ctrl.Result{}, nil
}

func (r *LogicalReplicationReconciler) setFailedStatus(ctx context.Context,
	obj *replicationv1alpha1.LogicalReplication, err error) error {
	if err == nil {
		return nil
	}

	var reason string
	replerr, ok := err.(ReplicationError)
	if ok {
		reason = string(replerr.Reason)
	}

	obj.Status.ReplicationStatus = replicationv1alpha1.ReplicationStatus{
		Phase:   replicationv1alpha1.ReplicationPhaseFailed,
		Message: err.Error(),
		Reason:  reason,
	}

	patch := client.MergeFrom(obj)
	return r.Status().Patch(ctx, obj, patch)
}

// SetupWithManager sets up the controller with the Manager.
func (r *LogicalReplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&replicationv1alpha1.LogicalReplication{}).
		Complete(r)
}

type LogicalReplicationIteration struct {
	Client   client.Client
	ctx      context.Context
	Request  ctrl.Request
	log      logr.Logger
	obj      *replicationv1alpha1.LogicalReplication
	pubCreds replication.DatabaseCredentials
	pubDB    *sql.DB
	subCreds replication.DatabaseCredentials
	subDB    *sql.DB
}

func (i *LogicalReplicationIteration) Iterate(lr *replicationv1alpha1.LogicalReplication) error {
	i.log = log.FromContext(i.ctx)
	i.obj = lr

	err := i.readCredentails()
	if err != nil {
		return err
	}
	err = i.connectDBs()
	if err != nil {
		return err
	}
	err = i.checkPublication()
	if err != nil {
		return err
	}

	tables, err := i.publicationTables()
	if err != nil {
		return err
	}
	for _, table := range tables {
		i.log.Info("checking", table.Schema, table.Name)
		err = i.checkSubscriptionSchema(table)
		if err != nil {
			return err
		}
		err = i.checkSubscriptionTable(table)
		if err != nil {
			return err
		}
		err = i.checkSubscriptionView(table)
		if err != nil {
			return err
		}
	}

	err = i.checkSubscription()
	return err
}

func (i *LogicalReplicationIteration) readCredentails() error {
	publishingDb, err := i.getCredentialsFromSecret(i.obj.Spec.Publication.SecretName)
	if err != nil {
		i.log.Error(err, "getting publication credentials")
		return NewReplicationError(SecretError, err)
	}
	i.pubCreds = publishingDb

	i.log.Info("publishing database", "databaseHost", publishingDb.Host, "databasePort", publishingDb.Port)

	subscribingDb, err := i.getCredentialsFromSecret(i.obj.Spec.Subscription.SecretName)
	if err != nil {
		i.log.Error(err, "getting subscribing credentials")
		return NewReplicationError(SecretError, err)
	}
	i.subCreds = subscribingDb

	i.log.Info("subscribing database", "databaseHost", subscribingDb.Host, "databasePort", subscribingDb.Port)
	return nil
}

func (i *LogicalReplicationIteration) connectDB(creds replication.DatabaseCredentials) (*sql.DB, error) {
	db, err := replication.DBConnect(creds)
	if err != nil {
		i.log.Error(err, fmt.Sprintf("connecting to %s db", creds.DatabaseName))
		return nil, NewReplicationError(ConnectError, err)
	}
	i.log.Info(fmt.Sprintf("connected to %s database", creds.DatabaseName))
	return db, nil
}

func (i *LogicalReplicationIteration) connectDBs() error {
	var err error
	i.pubDB, err = i.connectDB(i.pubCreds)
	if err != nil {
		return err
	}

	i.subDB, err = i.connectDB(i.subCreds)
	return err
}

func (i *LogicalReplicationIteration) checkPublication() error {
	err := replication.CheckPublication(i.pubDB, i.obj.Spec.Publication.Name)
	if err != nil {
		i.log.Error(err, "checking publication")
		return NewReplicationError(PublicationError, err)
	}
	i.log.Info("checked publications")

	return nil
}

func (i *LogicalReplicationIteration) publicationTables() ([]replication.PgTable, error) {
	tables, err := replication.PublicationTables(i.pubDB, i.obj.Spec.Publication.Name)
	if err != nil {
		i.log.Error(err, "checking publication tables")
		return nil, NewReplicationError(PublicationTablesError, err)
	}
	i.log.Info("checked publication tables")

	return tables, nil
}

func (i *LogicalReplicationIteration) checkSubscriptionSchema(table replication.PgTable) error {
	err := replication.CheckSubscriptionSchema(i.subDB, table.Schema)
	if err != nil {
		i.log.Error(err, "checking subscription schema", "schema", table.Schema)
		return NewReplicationError(SubscriptionSchemaError, err)
	}
	i.log.Info("checked subscription schema", "schema", table.Schema)
	return nil
}

func (i *LogicalReplicationIteration) checkSubscriptionTable(table replication.PgTable) error {
	err := replication.PublicationTableDetail(i.pubDB, &table)
	if err != nil {
		i.log.Error(err, "reading publication table details", table.Schema, table.Name)
		return NewReplicationError(SubscriptionSchemaError, err)
	}
	i.log.Info("read publication table details", table.Schema, table.Name)

	err = replication.CheckSubscriptionTableDetail(i.subDB, i.obj.Spec.Publication.Name, table)
	if err != nil {
		i.log.Error(err, "checking publication table details", table.Schema, table.Name)
		return NewReplicationError(SubscriptionSchemaError, err)
	}
	i.log.Info("checking publication table details", table.Schema, table.Name)

	return nil
}

func (i *LogicalReplicationIteration) checkSubscriptionView(replication.PgTable) error {
	return nil
}

func (i *LogicalReplicationIteration) checkSubscription() error {
	err := replication.CheckSubscription(i.subDB, i.obj.Spec.Publication.Name, i.pubCreds)
	if err != nil {
		i.log.Error(err, "checking subscription")
		return NewReplicationError(SubscriptionError, err)
	}
	i.log.Info("checked subscription")

	return nil
}

// Get secret with database credentials by name
func (i *LogicalReplicationIteration) getCredentialsFromSecret(secretName string) (replication.DatabaseCredentials, error) {
	var db replication.DatabaseCredentials
	var secret corev1.Secret
	var err error

	nn := types.NamespacedName{
		Name:      secretName,
		Namespace: i.Request.Namespace,
	}
	if err = i.Client.Get(i.ctx, nn, &secret); err != nil {
		return db, err
	}

	var data interface{}
	if len(secret.Data) > 0 {
		data = secret.Data
	} else if len(secret.StringData) > 0 {
		data = secret.StringData
	} else {
		return db, fmt.Errorf("no secret data")
	}

	err = mapstructure.WeakDecode(data, &db)
	return db, err
}

func NewLogicalReplicationIteration(client client.Client, ctx context.Context, req ctrl.Request) *LogicalReplicationIteration {
	return &LogicalReplicationIteration{
		Client:  client,
		ctx:     ctx,
		Request: req,
	}
}
