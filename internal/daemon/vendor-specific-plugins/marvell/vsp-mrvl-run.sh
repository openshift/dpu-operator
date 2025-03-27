#!/usr/bin/bash

set -x

load_driver() {
    chk=$(chroot /host lsmod | grep -w octeon_ep)
    if [ -z $chk ]
    then
        chroot /host modprobe octeon_ep
        sleep 1
        echo "Loaded octeon_ep driver"
    fi

    chk=$(chroot /host lsmod | grep -w octeon_ep_vf)
    if [ -z $chk ]
    then
        chroot /host modprobe octeon_ep_vf
        sleep 1
        echo "Loaded octeon_ep_vf driver"
    fi
}

unload_driver() {
    chroot /host rmmod octeon_ep_vf -f
    chk=$(chroot /host lsmod | grep -w octeon_ep)
    if [ -n $chk ]
    then
        chroot /host rmmod octeon_ep -f
        echo "Unloaded octeon_ep and octeon_ep_vf drivers, wait for 20 seconds before retry"
        sleep 20
    fi
}

detect_link() {
    pf="0000:$(lspci -d 177d:b900 -n | awk 'NR==1{print $1}')"
    path="/sys/bus/pci/devices/$pf/net/"
    ifname=$(ls $path)

    state=$(cat /sys/class/net/$ifname/operstate)
    if [ $state == "up" ]
    then
        echo "PF found: $ifname"
        return 0
    else
        return 1
    fi
}

setup_host_link() {
    while true; do
        detect_link
        if [ $? -eq 0 ]
        then
            echo "Link is up"
            break
        else
            echo "Link is down"
            unload_driver
            load_driver
        fi
    done
}

setup_hugepages() {
    if ! mount | grep -q "^none on /dev/huge type hugetlbfs " ; then
        mkdir -p /dev/huge
        mount -t hugetlbfs none /dev/huge
    fi
}

setup_dpu_link() {
    setup_hugepages
    chroot /host modprobe vfio-pci
    dpi=$(lspci -d 177d:a080 -n | awk 'NR==1{print $1}')
    if [ -z $dpi ]
    then
        echo "DPI device not found"
        return 1
    fi

    echo "DPI device: $dpi"

    pem=$(lspci -d 177d:a06c -n | awk 'NR==1{print $1}')
    if [ -z $pem ]
    then
        echo "PEM device not found"
        return 1
    fi

    echo "PEM device: $pem"

    echo $dpi > /sys/bus/pci/drivers/mrvl_cn10k_dpi/unbind || :
    echo vfio-pci > /sys/bus/pci/devices/$dpi/driver_override && echo $dpi > /sys/bus/pci/drivers_probe
    echo vfio-pci > /sys/bus/pci/devices/$pem/driver_override && echo $pem > /sys/bus/pci/drivers_probe

    exec /usr/bin/octep_cp_agent /usr/bin/cn106xx.cfg -- --dpi_dev $dpi --pem_dev $pem &> /tmp/octep-cp-log.txt &

    return 0

}


run() {
    if [ "$(lspci -d 177d:a0f7)" != "" ] ; then
        mode="dpu"
	echo "mode: dpu"
    else
	mode="host"
	echo "mode: host"
    fi

    if test "$mode" = "host" ; then
        setup_host_link
    else
        setup_dpu_link
        if [ $? -ne 0 ]; then
            echo "Failed to set up CP Agent"
            return
        fi
    fi

    /vsp-mrvl
}

run
