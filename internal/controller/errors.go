package controller

type ReplicationErrorReason string

var SecretError ReplicationErrorReason = "SecretError"

type ReplicationError struct {
	Reason ReplicationErrorReason
	Err    error
}

func (re ReplicationError) Error() string {
	return re.Err.Error()
}

func NewReplicationError(reason ReplicationErrorReason, err error) ReplicationError {
	return ReplicationError{Reason: reason, Err: err}
}
