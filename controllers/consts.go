package controllers

const (
	MachineReadyCondition string = "MachineReady"

	WaitingForMachineReadyConditionReason string = "WaitingForMachineReadyCondition"
	WaitingForNodeHealthyConditionReason  string = "WaitingForNodeHealthyCondition"
	NoReasonReason                        string = "NoReason"
)
