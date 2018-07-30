// Copyright (c) 2017 Intel Corporation
// Copyright (c) 2018 HyperHQ Inc.
//
// SPDX-License-Identifier: Apache-2.0
//

package kata

import (
	"errors"
	"fmt"
	"io/ioutil"
	goruntime "runtime"
	"strings"

	"github.com/BurntSushi/toml"
	vc "github.com/kata-containers/runtime/virtcontainers"
	"github.com/kata-containers/runtime/virtcontainers/pkg/oci"
)

var defaultHypervisorPath = "/usr/bin/qemu-lite-system-x86_64"
var defaultImagePath = "/usr/share/kata-containers/kata-containers.img"
var defaultKernelPath = "/usr/share/kata-containers/vmlinuz.container"
var defaultInitrdPath = "/usr/share/kata-containers/kata-containers-initrd.img"
var defaultFirmwarePath = ""
var defaultMachineAccelerators = ""

const defaultKernelParams = ""
const defaultMachineType = "pc"
const defaultRootDirectory = "/var/run/kata-containers"
const systemdUnitName = "kata-containers.target"

const defaultVCPUCount uint32 = 1
const defaultMaxVCPUCount uint32 = 0
const defaultMemSize uint32 = 2048 // MiB
const defaultBridgesCount uint32 = 1
const defaultInterNetworkingModel = "macvtap"
const defaultDisableBlockDeviceUse bool = false
const defaultBlockDeviceDriver = "virtio-scsi"
const defaultEnableIOThreads bool = false
const defaultEnableMemPrealloc bool = false
const defaultEnableHugePages bool = false
const defaultEnableSwap bool = false
const defaultEnableDebug bool = false
const defaultDisableNestingChecks bool = false
const defaultMsize9p uint32 = 8192

// Default config file used by stateless systems.
var defaultRuntimeConfiguration = "/usr/share/defaults/kata-containers/configuration.toml"

// Alternate config file that takes precedence over
// defaultRuntimeConfiguration.
var defaultSysConfRuntimeConfiguration = "/etc/kata-containers/configuration.toml"

const (
	defaultHypervisor = vc.QemuHypervisor
	defaultProxy      = vc.KataBuiltInProxyType
	defaultShim       = vc.KataBuiltInShimType
	defaultAgent      = vc.KataContainersAgent
)

// The TOML configuration file contains a number of sections (or
// tables). The names of these tables are in dotted ("nested table")
// form:
//
//   [<component>.<type>]
//
// The components are hypervisor, proxy, shim and agent. For example,
//
//   [proxy.kata]
//
// The currently supported types are listed below:
const (
	// supported hypervisor component types
	qemuHypervisorTableType = "qemu"

	// the maximum amount of PCI bridges that can be cold plugged in a VM
	maxPCIBridges uint32 = 5
)

type tomlConfig struct {
	Hypervisor map[string]hypervisor
	Proxy      map[string]proxy
	Shim       map[string]shim
	Agent      map[string]agent
	Runtime    runtime
	Factory    factory
}

type factory struct {
	Template bool `toml:"enable_template"`
}

type hypervisor struct {
	Path                  string `toml:"path"`
	Kernel                string `toml:"kernel"`
	Initrd                string `toml:"initrd"`
	Image                 string `toml:"image"`
	Firmware              string `toml:"firmware"`
	MachineAccelerators   string `toml:"machine_accelerators"`
	KernelParams          string `toml:"kernel_params"`
	MachineType           string `toml:"machine_type"`
	DefaultVCPUs          int32  `toml:"default_vcpus"`
	DefaultMaxVCPUs       uint32 `toml:"default_maxvcpus"`
	DefaultMemSz          uint32 `toml:"default_memory"`
	DefaultBridges        uint32 `toml:"default_bridges"`
	Msize9p               uint32 `toml:"msize_9p"`
	BlockDeviceDriver     string `toml:"block_device_driver"`
	DisableBlockDeviceUse bool   `toml:"disable_block_device_use"`
	MemPrealloc           bool   `toml:"enable_mem_prealloc"`
	HugePages             bool   `toml:"enable_hugepages"`
	Swap                  bool   `toml:"enable_swap"`
	Debug                 bool   `toml:"enable_debug"`
	DisableNestingChecks  bool   `toml:"disable_nesting_checks"`
	EnableIOThreads       bool   `toml:"enable_iothreads"`
}

type proxy struct {
	Path  string `toml:"path"`
	Debug bool   `toml:"enable_debug"`
}

type runtime struct {
	Debug             bool   `toml:"enable_debug"`
	InterNetworkModel string `toml:"internetworking_model"`
}

type shim struct {
	Path  string `toml:"path"`
	Debug bool   `toml:"enable_debug"`
}

type agent struct {
}

func (h hypervisor) path() (string, error) {
	p := h.Path

	if h.Path == "" {
		p = defaultHypervisorPath
	}

	return resolvePath(p)
}

func (h hypervisor) kernel() (string, error) {
	p := h.Kernel

	if p == "" {
		p = defaultKernelPath
	}

	return resolvePath(p)
}

func (h hypervisor) initrd() (string, error) {
	p := h.Initrd

	if p == "" {
		return "", nil
	}

	return resolvePath(p)
}

func (h hypervisor) image() (string, error) {
	p := h.Image

	if p == "" {
		return "", nil
	}

	return resolvePath(p)
}

func (h hypervisor) firmware() (string, error) {
	p := h.Firmware

	if p == "" {
		if defaultFirmwarePath == "" {
			return "", nil
		}
		p = defaultFirmwarePath
	}

	return resolvePath(p)
}

func (h hypervisor) machineAccelerators() string {
	var machineAccelerators string
	accelerators := strings.Split(h.MachineAccelerators, ",")
	acceleratorsLen := len(accelerators)
	for i := 0; i < acceleratorsLen; i++ {
		if accelerators[i] != "" {
			machineAccelerators += strings.Trim(accelerators[i], "\r\t\n ") + ","
		}
	}

	machineAccelerators = strings.Trim(machineAccelerators, ",")

	return machineAccelerators
}

func (h hypervisor) kernelParams() string {
	if h.KernelParams == "" {
		return defaultKernelParams
	}

	return h.KernelParams
}

func (h hypervisor) machineType() string {
	if h.MachineType == "" {
		return defaultMachineType
	}

	return h.MachineType
}

func (h hypervisor) defaultVCPUs() uint32 {
	numCPUs := goruntime.NumCPU()

	if h.DefaultVCPUs < 0 || h.DefaultVCPUs > int32(numCPUs) {
		return uint32(numCPUs)
	}
	if h.DefaultVCPUs == 0 { // or unspecified
		return defaultVCPUCount
	}

	return uint32(h.DefaultVCPUs)
}

func (h hypervisor) defaultMaxVCPUs() uint32 {
	numcpus := uint32(goruntime.NumCPU())
	maxvcpus := vc.MaxQemuVCPUs()
	reqVCPUs := h.DefaultMaxVCPUs

	//don't exceed the number of physical CPUs. If a default is not provided, use the
	// numbers of physical CPUs
	if reqVCPUs >= numcpus || reqVCPUs == 0 {
		reqVCPUs = numcpus
	}

	// Don't exceed the maximum number of vCPUs supported by hypervisor
	if reqVCPUs > maxvcpus {
		return maxvcpus
	}

	return reqVCPUs
}

func (h hypervisor) defaultMemSz() uint32 {
	if h.DefaultMemSz < 8 {
		return defaultMemSize // MiB
	}

	return h.DefaultMemSz
}

func (h hypervisor) defaultBridges() uint32 {
	if h.DefaultBridges == 0 {
		return defaultBridgesCount
	}

	if h.DefaultBridges > maxPCIBridges {
		return maxPCIBridges
	}

	return h.DefaultBridges
}

func (h hypervisor) blockDeviceDriver() (string, error) {
	if h.BlockDeviceDriver == "" {
		return defaultBlockDeviceDriver, nil
	}

	if h.BlockDeviceDriver != vc.VirtioSCSI && h.BlockDeviceDriver != vc.VirtioBlock {
		return "", fmt.Errorf("Invalid value %s provided for hypervisor block storage driver, can be either %s or %s", h.BlockDeviceDriver, vc.VirtioSCSI, vc.VirtioBlock)
	}

	return h.BlockDeviceDriver, nil
}

func (h hypervisor) msize9p() uint32 {
	if h.Msize9p == 0 {
		return defaultMsize9p
	}

	return h.Msize9p
}

func newQemuHypervisorConfig(h hypervisor) (vc.HypervisorConfig, error) {
	hypervisor, err := h.path()
	if err != nil {
		return vc.HypervisorConfig{}, err
	}

	kernel, err := h.kernel()
	if err != nil {
		return vc.HypervisorConfig{}, err
	}

	initrd, err := h.initrd()
	if err != nil {
		return vc.HypervisorConfig{}, err
	}

	image, err := h.image()
	if err != nil {
		return vc.HypervisorConfig{}, err
	}

	if image != "" && initrd != "" {
		return vc.HypervisorConfig{},
			errors.New("cannot specify an image and an initrd in configuration file")
	}

	firmware, err := h.firmware()
	if err != nil {
		return vc.HypervisorConfig{}, err
	}

	machineAccelerators := h.machineAccelerators()
	kernelParams := h.kernelParams()
	machineType := h.machineType()

	blockDriver, err := h.blockDeviceDriver()
	if err != nil {
		return vc.HypervisorConfig{}, err
	}

	return vc.HypervisorConfig{
		HypervisorPath:        hypervisor,
		KernelPath:            kernel,
		InitrdPath:            initrd,
		ImagePath:             image,
		FirmwarePath:          firmware,
		MachineAccelerators:   machineAccelerators,
		KernelParams:          vc.DeserializeParams(strings.Fields(kernelParams)),
		HypervisorMachineType: machineType,
		DefaultVCPUs:          h.defaultVCPUs(),
		DefaultMaxVCPUs:       h.defaultMaxVCPUs(),
		DefaultMemSz:          h.defaultMemSz(),
		DefaultBridges:        h.defaultBridges(),
		DisableBlockDeviceUse: h.DisableBlockDeviceUse,
		MemPrealloc:           h.MemPrealloc,
		HugePages:             h.HugePages,
		Mlock:                 !h.Swap,
		Debug:                 h.Debug,
		DisableNestingChecks:  h.DisableNestingChecks,
		BlockDeviceDriver:     blockDriver,
		EnableIOThreads:       h.EnableIOThreads,
		Msize9p:               h.msize9p(),
	}, nil
}

func newFactoryConfig(f factory) (oci.FactoryConfig, error) {
	return oci.FactoryConfig{Template: f.Template}, nil
}

func updateRuntimeConfig(configPath string, tomlConf tomlConfig, config *oci.RuntimeConfig) error {
	for k, hypervisor := range tomlConf.Hypervisor {
		switch k {
		case qemuHypervisorTableType:
			hConfig, err := newQemuHypervisorConfig(hypervisor)
			if err != nil {
				return fmt.Errorf("%v: %v", configPath, err)
			}

			config.VMConfig.Memory = uint(hConfig.DefaultMemSz)

			config.HypervisorConfig = hConfig
		}
	}

	fConfig, err := newFactoryConfig(tomlConf.Factory)
	if err != nil {
		return fmt.Errorf("%v: %v", configPath, err)
	}
	config.FactoryConfig = fConfig

	return nil
}

// loadConfiguration loads the configuration file and converts it into a
// runtime configuration.
//
// All paths are resolved fully meaning if this function does not return an
// error, all paths are valid at the time of the call.
func loadConfiguration() (config *oci.RuntimeConfig, err error) {
	defaultHypervisorConfig := vc.HypervisorConfig{
		HypervisorPath:        defaultHypervisorPath,
		KernelPath:            defaultKernelPath,

		//use the initrd instead of image by default, this
		//default can be changed by configure file.
		ImagePath:             "",
		InitrdPath:            defaultInitrdPath,
		FirmwarePath:          defaultFirmwarePath,
		MachineAccelerators:   defaultMachineAccelerators,
		HypervisorMachineType: defaultMachineType,
		DefaultVCPUs:          defaultVCPUCount,
		DefaultMaxVCPUs:       defaultMaxVCPUCount,
		DefaultMemSz:          defaultMemSize,
		DefaultBridges:        defaultBridgesCount,
		MemPrealloc:           defaultEnableMemPrealloc,
		HugePages:             defaultEnableHugePages,
		Mlock:                 !defaultEnableSwap,
		Debug:                 defaultEnableDebug,
		DisableNestingChecks:  defaultDisableNestingChecks,
		BlockDeviceDriver:     defaultBlockDeviceDriver,
		EnableIOThreads:       defaultEnableIOThreads,
		Msize9p:               defaultMsize9p,
	}

	defaultAgentConfig := vc.KataAgentConfig{LongLiveConn: true}

	config = &oci.RuntimeConfig{
		HypervisorType:   defaultHypervisor,
		HypervisorConfig: defaultHypervisorConfig,
		AgentType:        defaultAgent,
		AgentConfig:      defaultAgentConfig,
		ProxyType:        defaultProxy,
		ShimType:         defaultShim,
	}

	err = config.InterNetworkModel.SetModel(defaultInterNetworkingModel)
	if err != nil {
		return  config, err
	}

	var resolved string

	resolved, err = getDefaultConfigFile()

	if err == nil && resolved != "" {

		configData, err := ioutil.ReadFile(resolved)
		if err != nil {
			return config, err
		}

		var tomlConf tomlConfig
		_, err = toml.Decode(string(configData), &tomlConf)
		if err != nil {
			return config, err
		}

		if tomlConf.Runtime.InterNetworkModel != "" {
			err = config.InterNetworkModel.SetModel(tomlConf.Runtime.InterNetworkModel)
			if err != nil {
				return config, err
			}
		}

		if err := updateRuntimeConfig(resolved, tomlConf, config); err != nil {
			return  config, err
		}
	}

	return config, nil
}

// getDefaultConfigFilePaths returns a list of paths that will be
// considered as configuration files in priority order.
func getDefaultConfigFilePaths() []string {
	return []string{
		// normally below "/etc"
		defaultSysConfRuntimeConfiguration,

		// normally below "/usr/share"
		defaultRuntimeConfiguration,
	}
}

// getDefaultConfigFile looks in multiple default locations for a
// configuration file and returns the resolved path for the first file
// found, or an error if no config files can be found.
func getDefaultConfigFile() (string, error) {
	var errs []string

	for _, file := range getDefaultConfigFilePaths() {
		resolved, err := resolvePath(file)
		if err == nil {
			return resolved, nil
		}
		s := fmt.Sprintf("config file %q unresolvable: %v", file, err)
		errs = append(errs, s)
	}

	return "", errors.New(strings.Join(errs, ", "))
}