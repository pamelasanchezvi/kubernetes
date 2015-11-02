package vmt

import (
	cadvisor "github.com/google/cadvisor/info/v1"
)

type Host struct {
	IP       string
	Port     int
	Resource string
}

type Container struct {
	Hostname   string
	ExternalID string
	Name       string
	Image      string
	Spec       ContainerSpec
	Stats      []*ContainerStats
}

type ContainerSpec struct {
	cadvisor.ContainerSpec
	CpuRequest    int64
	MemoryRequest int64
}

type ContainerStats struct {
	cadvisor.ContainerStats
}
