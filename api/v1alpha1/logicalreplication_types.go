package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// LogicalReplicationSpec defines the desired state of LogicalReplication
type LogicalReplicationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	//
	Publication PublicationSpec `json:"publication"`

	//
	Subscription SubscriptionSpec `json:"subscription"`
}

// PublicationSpec defines the publisher connection information including
// name of the publication and the connection secret.
type PublicationSpec struct {
	// Name of the publication on the publisher's side
	Name string `json:"name"`

	// The secret name of to connect to the publisher's database
	SecretName string `json:"secretName"`
}

// SubscriptionSpec defines the database where the replication would be set up.
type SubscriptionSpec struct {
	// The secret name of to connect to the dababase where the replication
	// would be set up.
	SecretName string `json:"secretName"`
}

// last successfully reconciled values
type ReconciledValues struct {
	PublicationName        string
	PublicationSecretHash  string
	SubscriptionSecretHash string
}

// LogicalReplicationStatus defines the observed state of LogicalReplication
type LogicalReplicationStatus struct {
	ReplicationStatus ReplicationStatus `json:"replicationStatus"`
	ReconciledValues  ReconciledValues  `json:"reconciledValues"`
}

// Status of the replication
type ReplicationStatus struct {
	Phase   ReplicationPhase `json:"phase,omitempty"`
	Reason  string           `json:"reason,omitempty"`
	Message string           `json:"message,omitempty"`
}

// +kubebuilder:validation:Enum=Pending;Replicating;Failed;Unknown
type ReplicationPhase string

var ReplicationPhaseFailed = ReplicationPhase("Failed")

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// LogicalReplication is the Schema for the logicalreplications API
type LogicalReplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LogicalReplicationSpec   `json:"spec,omitempty"`
	Status LogicalReplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LogicalReplicationList contains a list of LogicalReplication
type LogicalReplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LogicalReplication `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LogicalReplication{}, &LogicalReplicationList{})
}
