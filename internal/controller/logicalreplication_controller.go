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

	currentValues, err := iteration.Iterate(lr)
	if err != nil {
		statusErr := r.setFailedStatus(ctx, lr, err)
		return ctrl.Result{Requeue: true}, statusErr
	}

	err = r.setReconciledValues(ctx, lr, currentValues)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
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

func (r *LogicalReplicationReconciler) setReconciledValues(ctx context.Context,
	obj *replicationv1alpha1.LogicalReplication, values *replicationv1alpha1.ReconciledValues) error {
	if values == nil {
		return nil
	}
	obj.Status.ReconciledValues = *values
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

func (i *LogicalReplicationIteration) Iterate(lr *replicationv1alpha1.LogicalReplication) (
	*replicationv1alpha1.ReconciledValues, error) {
	i.log = log.FromContext(i.ctx)
	i.obj = lr

	if err := i.readCredentails(); err != nil {
		return nil, err
	}

	if err := i.connectDBs(); err != nil {
		return nil, err
	}

	if err := i.checkPublication(); err != nil {
		return nil, err
	}

	if i.publicationChanged() {
		if err := i.renameTables(); err != nil {
			return nil, err
		}

		if err := i.disableOldSubscription(); err != nil {
			return nil, err
		}
	}

	tables, err := i.publicationTables()
	if err != nil {
		return nil, err
	}
	for _, table := range tables {

		if err = i.checkSubscriptionSchema(table); err != nil {
			return nil, err
		}

		if err = i.checkSubscriptionTable(table); err != nil {
			return nil, err
		}

	}

	if err := i.checkSubscription(); err != nil {
		return nil, err
	}

	for _, table := range tables {
		if err = i.checkSubscriptionView(table); err != nil {
			return nil, err
		}
	}

	values := i.currentValues(tables)
	return values, nil
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

func (i *LogicalReplicationIteration) publicationChanged() bool {
	return i.obj.Spec.Publication.Name != i.obj.Status.ReconciledValues.PublicationName
}

func (i *LogicalReplicationIteration) renameTables() error {
	for _, table := range i.obj.Status.ReconciledValues.Tables {
		// rename only if old table exist and renamed table does not

		err := replication.CheckSubscriptionTable(i.subDB, table)
		if err == sql.ErrNoRows { // only report missing table and go to next
			i.log.Error(err, "missing old subscription", "schema", table.Schema, "table", table.Name)
			continue
		} else if err != nil {
			i.log.Error(err, "renaming old subscription", "schema", table.Schema, "table", table.Name)
			return NewReplicationError(SubscriptionTablesError, err)
		}

		newTable := replication.PgTable{
			Schema: table.Schema,
			Name:   table.Name + "_" + i.obj.Status.ReconciledValues.PublicationName,
		}
		err = replication.CheckSubscriptionTable(i.subDB, newTable)
		if err == nil { // table has been already renamed, go to next
			continue
		} else if err != sql.ErrNoRows {
			i.log.Error(err, "renaming old subscription", "schema", table.Schema, "table", table.Name)
			return NewReplicationError(SubscriptionTablesError, err)
		}

		err = replication.RenameSubscriptionTable(i.subDB, table, newTable)
		if err != nil {
			i.log.Error(err, "renaming old subscription", "schema", table.Schema, "table", table.Name)
			return NewReplicationError(SubscriptionTablesError, err)
		}
	}
	return nil
}

func (i *LogicalReplicationIteration) disableOldSubscription() error {
	oldName := i.obj.Status.ReconciledValues.PublicationName
	if oldName == "" {
		return nil
	}

	if err := replication.CheckSubscription(i.subDB, oldName, ""); err != nil {
		if err == sql.ErrNoRows {
			i.log.Error(err, "old subscription does not exist", "subscription", oldName)
			return nil
		}
		i.log.Error(err, "checking", "subscription", oldName)
		return NewReplicationError(SubscriptionError, err)
	}

	if err := replication.DisableSubscription(i.subDB, oldName); err != nil {
		i.log.Error(err, "disabling", "subscription", oldName)
		return NewReplicationError(SubscriptionError, err)
	}
	i.log.Info("disabled", "subscription", oldName)

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
		if err == sql.ErrNoRows {
			err = replication.CreateSubscriptionSchema(i.subDB, table.Schema)
			if err != nil {
				i.log.Error(err, "creating subscription", "schema", table.Name)
				return NewReplicationError(SubscriptionError, err)
			}
			i.log.Info("created subscription", "schema", table.Name)
		} else {
			i.log.Error(err, "checking subscription", "schema", table.Schema)
			return NewReplicationError(SubscriptionSchemaError, err)
		}
	}

	i.log.Info("checked subscription", "schema", table.Schema)
	return nil
}

func (i *LogicalReplicationIteration) checkSubscriptionTable(table replication.PgTable) error {
	tableDetail, err := replication.PublicationTableDetail(i.pubDB, table)
	if err != nil {
		i.log.Error(err, "reading publication details", "schema", table.Schema, "table", table.Name)
		return NewReplicationError(PublicationTablesError, err)
	}
	i.log.Info("read publication details", "schema", table.Schema, "table", table.Name)

	err = replication.CheckSubscriptionTable(i.subDB, table)
	if err == sql.ErrNoRows {
		err = replication.CreateSubscriptionTable(i.subDB, tableDetail)
		if err != nil {
			i.log.Error(err, "creating subscription", "schema", table.Schema, "table", table.Name)
			return NewReplicationError(SubscriptionTablesError, err)
		}
	} else if err != nil {
		i.log.Error(err, "reading subscription", "schema", table.Schema, "table", table.Name)
		return NewReplicationError(SubscriptionTablesError, err)
	}

	err = replication.CheckSubscriptionTableDetail(i.subDB, tableDetail)
	if err != nil {
		i.log.Error(err, "reading subscription details", "schema", table.Schema, "table", table.Name)
		return NewReplicationError(SubscriptionTablesError, err)
	}

	i.log.Info("checking publication details", "schema", table.Schema, "table", table.Name)
	return nil
}

func (i *LogicalReplicationIteration) checkSubscriptionView(replication.PgTable) error {
	return nil
}

func (i *LogicalReplicationIteration) checkSubscription() error {
	connStr := replication.CredentialsToConnectionString(i.pubCreds)
	name := i.obj.Spec.Publication.Name

	err := replication.CheckSubscription(i.subDB, name, connStr)
	if err != nil {
		switch err {
		case sql.ErrNoRows:
			err = replication.CreateSubscription(i.subDB, name, connStr)
			if err != nil {
				i.log.Error(err, "recreating", "subscription", name)
				return NewReplicationError(SubscriptionError, err)
			}
			i.log.Info("created", "subscription", name)

		case replication.ErrWrongAttributes:
			i.log.Info("wrong attributes", "subscription", name)
			err = replication.AlterSubscription(i.subDB, name, connStr)
			if err != nil {
				i.log.Error(err, "altering", "subscription", name)
				return NewReplicationError(SubscriptionError, err)
			}

		default:
			i.log.Error(err, "checking", "subscription", name)
			return NewReplicationError(SubscriptionError, err)
		}
		err = replication.EnableSubscription(i.subDB, name, connStr)
		if err != nil {
			i.log.Error(err, "enabling", "subscription", name)
			return NewReplicationError(SubscriptionError, err)
		}
	}
	i.log.Info("checked", "subscription", name)

	return nil
}

func (i *LogicalReplicationIteration) currentValues(tables []replication.PgTable) *replicationv1alpha1.ReconciledValues {
	values := replicationv1alpha1.ReconciledValues{
		PublicationName:        i.obj.Spec.Publication.Name,
		PublicationSecretHash:  replication.Checksum(replication.CredentialsToConnectionString(i.pubCreds)),
		SubscriptionSecretHash: replication.Checksum(replication.CredentialsToConnectionString(i.subCreds)),
		Tables:                 tables,
	}

	return &values
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
