package controllers

const (
	MachineReadyCondition             string = "MachineReady"
	ClusterReadyCondition             string = "ClusterReady"
	ClusterControlPlaneReadyCondition string = "ClusterControlPlaneReady"
	ClusterComponentsReadyCondition   string = "ClusterComponentsReady"

	WaitingForMachineReadyConditionReason             string = "WaitingForMachineReadyCondition"
	WaitingForNodeHealthyConditionReason              string = "WaitingForNodeHealthyCondition"
	WaitingForClusterReadyConditionReason             string = "WaitingForClusterReadyCondition"
	WaitingForClusterControlPlaneReadyConditionReason string = "WaitingForClusterControlPlaneReadyCondition"
	WaitingForClusterComponentReadyConditionReason    string = "WaitingForClusterComponentReadyCondition"
	WaitingForClusterFallbackReason                   string = "WaitingForCluster"
	WaitingForClusterControlPlaneFallbackReason       string = "WaitingForClusterControlPlane"
)
