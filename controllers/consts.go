package controllers

const (
	MachineReadyCondition string = "MachineReady"
	ClusterReadyCondition string = "ClusterReady"

	WaitingForMachineReadyConditionReason string = "WaitingForMachineReadyCondition"
	WaitingForNodeHealthyConditionReason  string = "WaitingForNodeHealthyCondition"
	WaitingForClusterReadyConditionReason string = "WaitingForClusterReadyCondition"
	NoReasonReason                        string = "NoReason"
)
