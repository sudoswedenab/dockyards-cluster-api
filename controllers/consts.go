package controllers

const (
	MachineReadyCondition             string = "MachineReady"
	ClusterReadyCondition             string = "ClusterReady"
	ClusterControlPlaneReadyCondition string = "ClusterControlPlaneReady"
	ClusterComponentsReadyCondition   string = "ClusterComponentsReadyCondition"

	WaitingForMachineReadyConditionReason             string = "WaitingForMachineReadyCondition"
	WaitingForNodeHealthyConditionReason              string = "WaitingForNodeHealthyCondition"
	WaitingForClusterReadyConditionReason             string = "WaitingForClusterReadyCondition"
	WaitingForClusterControlPlaneReadyConditionReason string = "WaitingForClusterControlPlaneReadyCondition"
	WaitingForClusterComponentReadyConditionReason    string = "WaitingForClusterComponentReadyCondition"
	NoReasonReason                                    string = "NoReason"
)
