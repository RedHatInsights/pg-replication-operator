package controller

import (
	"context"
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
		r.setFailedStatus(lr, err)
		err := r.Status().Update(ctx, lr)
		return ctrl.Result{Requeue: true}, err
	}

	return ctrl.Result{}, nil
}

func (r *LogicalReplicationReconciler) setFailedStatus(obj *replicationv1alpha1.LogicalReplication, err error) {
	if err == nil {
		return
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
	pubCreds DatabaseCredentials
	subCreds DatabaseCredentials
}

func (i *LogicalReplicationIteration) Iterate(lr *replicationv1alpha1.LogicalReplication) error {
	i.log = log.FromContext(i.ctx)
	i.obj = lr

	err := i.readCredentails()
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

// Get secret with database credentials by name
func (i *LogicalReplicationIteration) getCredentialsFromSecret(secretName string) (DatabaseCredentials, error) {
	var db DatabaseCredentials
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
