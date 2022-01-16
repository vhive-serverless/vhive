// MIT License
//
// Copyright (c) 2020 Plamen Petrov and EASE lab
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package regular

import (
	"github.com/ease-lab/vhive/ctrimages"
	"github.com/ease-lab/vhive/devmapper"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/containerd/containerd"

	fcclient "github.com/firecracker-microvm/firecracker-containerd/firecracker-control/client"
	// note: from the original repo

	_ "google.golang.org/grpc/codes"  //tmp
	_ "google.golang.org/grpc/status" //tmp

	"github.com/ease-lab/vhive/memory/manager"
	"github.com/ease-lab/vhive/metrics"
	"github.com/ease-lab/vhive/misc"

	_ "github.com/davecgh/go-spew/spew" //tmp
)

const (
	containerdAddress      = "/run/firecracker-containerd/containerd.sock"
	containerdTTRPCAddress = containerdAddress + ".ttrpc"
	NamespaceName          = "firecracker-containerd"
)

type WorkloadIoWriter struct {
	logger *log.Entry
}

func NewWorkloadIoWriter(vmID string) WorkloadIoWriter {
	return WorkloadIoWriter{log.WithFields(log.Fields{"vmID": vmID})}
}

func (wio WorkloadIoWriter) Write(p []byte) (n int, err error) {
	s := string(p)
	lines := strings.Split(s, "\n")
	for i := range lines {
		wio.logger.Info(string(lines[i]))
	}
	return len(p), nil
}

// RegOrchestrator Drives all VMs
type RegOrchestrator struct {
	vmPool       *misc.VMPool
	workloadIo   sync.Map // vmID string -> WorkloadIoWriter
	snapshotter  string
	client       *containerd.Client
	fcClient     *fcclient.Client
	devMapper    *devmapper.DeviceMapper
	imageManager *ctrimages.ImageManager
	// store *skv.KVStore
	snapshotsEnabled bool
	isUPFEnabled     bool
	isLazyMode       bool
	snapshotsDir     string
	isMetricsMode    bool
	hostIface        string

	memoryManager *manager.MemoryManager
}

// NewDedupOrchestrator Initializes a new orchestrator
func NewRegOrchestrator(snapshotter, hostIface, poolName, metadataDev string, netPoolSize int, opts ...OrchestratorOption) *RegOrchestrator { // TODO: args
	var err error

	o := new(RegOrchestrator)
	o.vmPool = misc.NewVMPool(hostIface, netPoolSize)
	o.snapshotter = snapshotter
	o.snapshotsDir = "/fccd/snapshots"
	o.hostIface = hostIface

	for _, opt := range opts {
		opt(o)
	}

	if _, err := os.Stat(o.snapshotsDir); err != nil {
		if !os.IsNotExist(err) {
			log.Panicf("Snapshot dir %s exists", o.snapshotsDir)
		}
	}

	if err := os.MkdirAll(o.snapshotsDir, 0777); err != nil {
		log.Panicf("Failed to create snapshots dir %s", o.snapshotsDir)
	}

	if o.GetUPFEnabled() {
		managerCfg := manager.MemoryManagerCfg{
			MetricsModeOn: o.isMetricsMode,
		}
		o.memoryManager = manager.NewMemoryManager(managerCfg)
	}

	log.Info("Creating containerd client")
	o.client, err = containerd.New(containerdAddress)
	if err != nil {
		log.Fatal("Failed to start containerd client", err)
	}
	log.Info("Created containerd client")

	log.Info("Creating firecracker client")
	o.fcClient, err = fcclient.New(containerdTTRPCAddress)
	if err != nil {
		log.Fatal("Failed to start firecracker client", err)
	}
	log.Info("Created firecracker client")

	o.devMapper = devmapper.NewDeviceMapper(o.client, poolName, metadataDev)

	o.imageManager = ctrimages.NewImageManager(o.client, o.snapshotter)

	return o
}

func (o *RegOrchestrator) setupCloseHandler() {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Info("\r- Ctrl+C pressed in Terminal")
		_ = o.StopActiveVMs()
		o.Cleanup()
		os.Exit(0)
	}()
}

// Cleanup Removes the bridges created by the VM pool's tap manager
// Cleans up snapshots directory
func (o *RegOrchestrator) Cleanup() {
	o.vmPool.CleanupNetwork()
	if err := os.RemoveAll(o.snapshotsDir); err != nil {
		log.Panic("failed to delete snapshots dir", err)
	}
}

// GetSnapshotsEnabled Returns the snapshots mode of the orchestrator
func (o *RegOrchestrator) GetSnapshotsEnabled() bool {
	return o.snapshotsEnabled
}

// GetUPFEnabled Returns the UPF mode of the orchestrator
func (o *RegOrchestrator) GetUPFEnabled() bool {
	return o.isUPFEnabled
}

// DumpUPFPageStats Dumps the memory manager's stats about the number of
// the unique pages and the number of the pages that are reused across invocations
func (o *RegOrchestrator) DumpUPFPageStats(vmID, functionName, metricsOutFilePath string) error {
	logger := log.WithFields(log.Fields{"vmID": vmID})
	logger.Debug("RegOrchestrator received DumpUPFPageStats")

	return o.memoryManager.DumpUPFPageStats(vmID, functionName, metricsOutFilePath)
}

// DumpUPFLatencyStats Dumps the memory manager's latency stats
func (o *RegOrchestrator) DumpUPFLatencyStats(vmID, functionName, latencyOutFilePath string) error {
	logger := log.WithFields(log.Fields{"vmID": vmID})
	logger.Debug("RegOrchestrator received DumpUPFPageStats")

	return o.memoryManager.DumpUPFLatencyStats(vmID, functionName, latencyOutFilePath)
}

// GetUPFLatencyStats Returns the memory manager's latency stats
func (o *RegOrchestrator) GetUPFLatencyStats(vmID string) ([]*metrics.Metric, error) {
	logger := log.WithFields(log.Fields{"vmID": vmID})
	logger.Debug("RegOrchestrator received DumpUPFPageStats")

	return o.memoryManager.GetUPFLatencyStats(vmID)
}

func (o *RegOrchestrator) getSnapshotFile(vmID string) string {
	return filepath.Join(o.getVMBaseDir(vmID), "snap_file")
}

func (o *RegOrchestrator) getMemoryFile(vmID string) string {
	return filepath.Join(o.getVMBaseDir(vmID), "mem_file")
}

func (o *RegOrchestrator) getWorkingSetFile(vmID string) string {
	return filepath.Join(o.getVMBaseDir(vmID), "working_set_pages")
}

func (o *RegOrchestrator) getVMBaseDir(vmID string) string {
	return filepath.Join(o.snapshotsDir, vmID)
}

func (o *RegOrchestrator) setupHeartbeat() {
	heartbeat := time.NewTicker(60 * time.Second)

	go func() {
		for {
			<-heartbeat.C
			log.Info("HEARTBEAT: number of active VMs: ", len(o.vmPool.GetVMMap()))
		} // for
	}() // go func
}