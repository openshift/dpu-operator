package ipuplugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	log "github.com/sirupsen/logrus"

	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type DevicePluginService struct {
	pb.UnimplementedDeviceServiceServer
	mode string
}

var (
	//TODO: Use (GetFilteredPfs), to find interface names to be excluded.
	//excluding d3(host-acc), reserving D4,D1(QSPF ports) D6-D8(for max 3 host VFs), D9-D10(for single NF)
	exclude = []string{"enp0s1f0", "enp0s1f0d1", "enp0s1f0d2", "enp0s1f0d3",
		"enp0s1f0d4", "enp0s1f0d5", "enp0s1f0d6",
		"enp0s1f0d7", "enp0s1f0d8", "enp0s1f0d9", "enp0s1f0d10"}
	sysClassNet      = "/sys/class/net"
	sysBusPciDevices = "/sys/bus/pci/devices"
	deviceCode       = "0x1452"
	deviceCodeVf     = "0x145c"
	intelVendor      = "0x8086"
	maxVfsSupported  = 64
)

/*
hostVfDevs uses a map(where key is pci-address(for example-> 0000:cb:00.6))
accDevs uses a map(where key is ACC netdev interface name(for example-> enp0s1f0d14))
*/
var hostVfDevs map[string]*pb.Device
var accDevs map[string]*pb.Device

func NewDevicePluginService(mode string) *DevicePluginService {
	return &DevicePluginService{mode: mode}
}

func (s *DevicePluginService) GetDevices(context.Context, *pb.Empty) (*pb.DeviceListResponse, error) {

	devices, err := discoverHostDevices(s.mode)
	if err != nil {
		return &pb.DeviceListResponse{}, err
	}

	response := &pb.DeviceListResponse{
		Devices: devices,
	}

	log.Debugf("GetDevices, response->%v\n", response)
	return response, nil
}

// Note: This function below was taken from open-source sriov plugin, which
// is also under the same apache license, with few additional changes.
// Returns a List containing PCI addr for all VF discovered in a given PF
func GetVFList(pf string) (vfList []string, err error) {
	vfList = make([]string, 0)
	pfDir := filepath.Join(sysBusPciDevices, pf)
	_, err = os.Lstat(pfDir)
	if err != nil {
		log.Errorf("error. Could not get PF directory information for device: %s, Err: %v", pf, err)
		err = fmt.Errorf("error. Could not get PF directory information for device: %s, Err: %v", pf, err)
		return
	}

	vfDirs, err := filepath.Glob(filepath.Join(pfDir, "virtfn*"))

	if err != nil {
		log.Errorf("error reading VF directories %v", err)
		err = fmt.Errorf("error reading VF directories %v", err)
		return
	}

	// Read all VF directory and get add VF PCI addr to the vfList
	for _, dir := range vfDirs {
		dirInfo, err := os.Lstat(dir)
		if err == nil && (dirInfo.Mode()&os.ModeSymlink != 0) {
			linkName, err := filepath.EvalSymlinks(dir)
			if err == nil {
				vfLink := filepath.Base(linkName)
				vfList = append(vfList, vfLink)
			}
		}
	}
	return
}

func GetVfDeviceCount(pciAddr string) (int, error) {
	vfList, err := GetVFList(pciAddr)
	if err != nil {
		log.Errorf("Error->%v, from GetVFList", err)
		return 0, fmt.Errorf("Error->%v, from GetVFList", err)
	}
	length := len(vfList)
	log.Infof("GetVFList count->%v", length)
	return length, nil
}

// Recommended to wait upto 2 seconds, for the max VFs(64) currently supported.
func pollToCheckVfDevices(pciAddr string, count int) error {
	var err error
	cnt := 0
	ticker := time.NewTicker(time.Second / 3)
	done := make(chan bool, 1)

	go func() {
		for _ = range ticker.C {
			cnt, err = GetVfDeviceCount(pciAddr)
			if cnt == count && err == nil {
				ticker.Stop()
				break
			}
		}
		done <- true
	}()

	timer := time.NewTimer(time.Second * 2)
	select {
	case <-done:
		log.Debugf("ticker Done")
		timer.Stop()
	case <-timer.C:
		log.Debugf("timer.C Done")
		ticker.Stop()
	}

	if cnt == count && err == nil {
		log.Debugf("pollToCheckVfDevices: match expected count, %v\n", cnt)
		return nil
	}
	log.Debugf("pollToCheckVfDevices: Timed out:attempt->%v, result->%v, err->%v\n", count, cnt, err)
	return fmt.Errorf("pollToCheckVfDevices: Timed out:attempt->%v, result->%v, err->%v\n", count, cnt, err)
}

func SetNumSriovVfs(mode string, pciAddr string, vfCnt int32) error {

	//Note: Upto 64 VFs have been validated.
	if vfCnt <= 0 || vfCnt > int32(maxVfsSupported) {
		return fmt.Errorf("SetNumSriovVfs(): Invalid/unsupported, vfCnt->%v \n", vfCnt)
	}

	pathToNumVfsFile := filepath.Join(sysBusPciDevices, pciAddr, "sriov_numvfs")

	//Need to first write 0 for num of VFs, before updating it.
	err := os.WriteFile(pathToNumVfsFile, []byte("0"), 0644)
	if err != nil {
		return fmt.Errorf("SetNumSriovVfs(): reset fail %s: %v", pathToNumVfsFile, err)
	}
	zeroVfs := 0
	err = pollToCheckVfDevices(pciAddr, zeroVfs)
	if err != nil {
		return fmt.Errorf("cli-client query failed count->%v, err->%v\n", zeroVfs, err)
	}

	// Note: Post-writing, it can take some time for the VFs to be created.
	err = os.WriteFile(pathToNumVfsFile, []byte(strconv.Itoa(int(vfCnt))), 0644)
	if err != nil {
		return fmt.Errorf("SetNumSriovVfs():error in updating %s: %v", pathToNumVfsFile, err)
	}

	err = pollToCheckVfDevices(pciAddr, int(vfCnt))
	if err != nil {
		return fmt.Errorf("cli-client query failed count->%v, err->%v\n", vfCnt, err)
	}

	log.Debugf("SetNumSriovVfs(): updated file->%s, sriov_numvfs to %v\n", pathToNumVfsFile, vfCnt)

	return nil
}

func SetNumVfs(mode string, numVfs int32) (int32, error) {
	deviceVfsSet := false

	if mode != types.HostMode {
		return 0, fmt.Errorf("setNumVfs(): only supported on host: mode %s\n", mode)
	}

	log.Debugf("setNumVfs(): requested num of VFs->%v\n", numVfs)

	files, err := os.ReadDir(sysBusPciDevices)
	if err != nil {
		return 0, fmt.Errorf("setNumVfs(): error-> %v\n", err)
	}

	for _, file := range files {
		deviceByte, err := os.ReadFile(filepath.Join(sysBusPciDevices, file.Name(), "device"))
		if err != nil {
			log.Errorf("Error reading PCIe deviceID: %s\n", err)
			continue
		}

		vendorByte, err := os.ReadFile(filepath.Join(sysBusPciDevices, file.Name(), "vendor"))
		if err != nil {
			log.Errorf("Error reading VendorID: %s\n", err)
			continue
		}

		deviceId := strings.TrimSpace(string(deviceByte))
		vendorId := strings.TrimSpace(string(vendorByte))

		if deviceId == deviceCode && vendorId == intelVendor {
			err = SetNumSriovVfs(mode, file.Name(), numVfs)
			if err != nil {
				return 0, fmt.Errorf("setNumVfs(): error from SetSriovNumVfs-> %v", err)
			}
			deviceVfsSet = true
			break
		}
	}
	if deviceVfsSet == true {
		return numVfs, nil
	} else {
		return 0, fmt.Errorf("setNumVfs(): unable to set VFs for device->%s", deviceCode)
	}
}

func (s *DevicePluginService) SetNumVfs(ctx context.Context, vfCountReq *pb.VfCount) (*pb.VfCount, error) {
	var res *pb.VfCount
	numVfs, err := SetNumVfs(types.HostMode, vfCountReq.VfCnt)

	log.Debugf("setNumVfs(): requested VFs->%v, allocated VFs->%v, err->%v\n", vfCountReq.VfCnt, numVfs, err)
	if err != nil {
		res = &pb.VfCount{VfCnt: 0}
	} else {
		res = &pb.VfCount{VfCnt: numVfs}
	}

	log.Debugf("SetNumVfs res->%v\n", res)
	return res, err
}

// GetPciFromNetDev takes in a network device name and returns its PCI address
// Note: This function(GetPciFromNetDev) is based on similar api in dpu-operator/dpu-cni/pkgs/sriovutils
func GetPciFromNetDev(ifName string) (string, error) {
	netDevPath := filepath.Join(sysClassNet, ifName, "device")
	pciAddr, err := filepath.EvalSymlinks(netDevPath)
	if err != nil {
		return "", fmt.Errorf("failed to find PCI address for net device %s: %v", ifName, err)
	}

	return filepath.Base(pciAddr), nil
}

/*
For the first call, we query the devices(on host or ACC) and cache it.
For subsequent calls, we return cached list of devices. Caching helps in
addressing the case, wherein, if host-VF is allocated for a pod, it is no
longer available in host netnamespace, so doesnt show up under /sys/class/net,
so we were returning 1 less device, but DPU's allocator still expects, the
overall count of Host-VFs(even if one of them is in allocated state).
DPU resource allocation, maintains its own list of total vs how-many VFs available.
*/
func discoverHostDevices(mode string) (map[string]*pb.Device, error) {

	if mode != types.IpuMode && mode != types.HostMode {
		return make(map[string]*pb.Device), fmt.Errorf("Invalid mode->%v", mode)
	}
	//Note: It is expected that VSP's-Init(on ACC) gets invoked prior to GetDevices,
	//this check is meant to catch any anomalies.
	if mode == types.IpuMode {
		if len(AccApfsAvailForCNI) == 0 {
			log.Errorf("discoverHostDevices: Error, AccApfsAvailForCNI not setup")
			return make(map[string]*pb.Device), fmt.Errorf("discoverHostDevices: Error, AccApfsAvailForCNI not setup")
		}
	}

	if mode == types.IpuMode {
		if accDevs == nil {
			accDevs = make(map[string]*pb.Device)
		} else if len(accDevs) > 0 {
			return accDevs, nil
		}
	} else { //mode == types.HostMode
		if hostVfDevs == nil {
			hostVfDevs = make(map[string]*pb.Device)
		} else if len(hostVfDevs) > 0 {
			return hostVfDevs, nil
		}
	}

	files, err := os.ReadDir(sysClassNet)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*pb.Device), nil
		}
	}

	for _, file := range files {
		deviceCodeByte, err := os.ReadFile(filepath.Join(sysClassNet, file.Name(), "device/device"))
		if err != nil {
			continue
		}

		device_code := strings.TrimSpace(string(deviceCodeByte))
		if mode == types.IpuMode {
			if device_code == deviceCode {
				if slices.Contains(AccApfsAvailForCNI, file.Name()) {
					accDevs[file.Name()] = &pb.Device{ID: file.Name(), Health: pluginapi.Healthy}
				}
			}
		} else if mode == types.HostMode {
			if device_code == deviceCodeVf {
				pciAddr, err := GetPciFromNetDev(file.Name())
				if err != nil {
					log.Errorf("Error->%v finding pci addr from netinterface->%s", err, file.Name())
					continue
				}
				hostVfDevs[pciAddr] = &pb.Device{ID: pciAddr, Health: pluginapi.Healthy}
			}
		}
	}

	if mode == types.IpuMode {
		return accDevs, nil
	}

	return hostVfDevs, nil
}
