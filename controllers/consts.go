package controllers

const (
	MachineReadyCondition             string = "MachineReady"
	ClusterReadyCondition             string = "ClusterReady"
	ClusterControlPlaneReadyCondition string = "ClusterControlPlaneReady"

	WaitingForMachineReadyConditionReason             string = "WaitingForMachineReadyCondition"
	WaitingForNodeHealthyConditionReason              string = "WaitingForNodeHealthyCondition"
	WaitingForClusterReadyConditionReason             string = "WaitingForClusterReadyCondition"
	WaitingForClusterControlPlaneReadyConditionReason string = "WaitingForClusterControlPlaneReadyCondition"
	NoReasonReason                                    string = "NoReason"
)
