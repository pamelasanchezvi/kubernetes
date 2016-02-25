package probe

type NodeResourceStat struct {
	cpuAllocationCapacity float64
	cpuAllocationUsed     float64
	memAllocationCapacity float64
	memAllocationUsed     float64
	vCpuCapacity          float64
	vCpuUsed              float64
	vMemCapacity          float64
	vMemUsed              float64
}

type PodResourceStat struct {
	cpuAllocationCapacity float64
	cpuAllocationUsed     float64
	memAllocationCapacity float64
	memAllocationUsed     float64
}
