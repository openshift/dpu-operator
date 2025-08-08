package utils_test

import (
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/spf13/afero"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("FilesystemModeDetector", func() {
	Describe("IsImageMode", func() {
		Context("when /host-run/ostree-booted exists", func() {
			It("should return true for image mode", func() {
				hostRunIMFs := afero.NewMemMapFs()
				hostRunIMFs.Create("/host-run/ostree-booted")

				fmd := utils.NewFilesystemModeDetectorWithFs(hostRunIMFs)
				imageMode, err := fmd.IsImageMode()

				Expect(err).NotTo(HaveOccurred())
				Expect(imageMode).To(BeTrue())
			})
		})

		Context("when /run/ostree-booted exists", func() {
			It("should return true for image mode", func() {
				runIMFs := afero.NewMemMapFs()
				runIMFs.Create("/run/ostree-booted")

				fmd := utils.NewFilesystemModeDetectorWithFs(runIMFs)
				imageMode, err := fmd.IsImageMode()

				Expect(err).NotTo(HaveOccurred())
				Expect(imageMode).To(BeTrue())
			})
		})

		Context("when both paths exist", func() {
			It("should return true for image mode (prioritizes /host-run)", func() {
				bothPathsIMFs := afero.NewMemMapFs()
				bothPathsIMFs.Create("/host-run/ostree-booted")
				bothPathsIMFs.Create("/run/ostree-booted")

				fmd := utils.NewFilesystemModeDetectorWithFs(bothPathsIMFs)
				imageMode, err := fmd.IsImageMode()

				Expect(err).NotTo(HaveOccurred())
				Expect(imageMode).To(BeTrue())
			})
		})

		Context("when only /host-run/ostree-booted exists", func() {
			It("should return true for image mode", func() {
				hostRunOnlyIMFs := afero.NewMemMapFs()
				hostRunOnlyIMFs.Create("/host-run/ostree-booted")

				fmd := utils.NewFilesystemModeDetectorWithFs(hostRunOnlyIMFs)
				imageMode, err := fmd.IsImageMode()

				Expect(err).NotTo(HaveOccurred())
				Expect(imageMode).To(BeTrue())
			})
		})

		Context("when only /run/ostree-booted exists", func() {
			It("should return true for image mode", func() {
				runOnlyIMFs := afero.NewMemMapFs()
				runOnlyIMFs.Create("/run/ostree-booted")

				fmd := utils.NewFilesystemModeDetectorWithFs(runOnlyIMFs)
				imageMode, err := fmd.IsImageMode()

				Expect(err).NotTo(HaveOccurred())
				Expect(imageMode).To(BeTrue())
			})
		})

		Context("when neither path exists", func() {
			It("should return false for image mode", func() {
				emptyFs := afero.NewMemMapFs()

				fmd := utils.NewFilesystemModeDetectorWithFs(emptyFs)
				imageMode, err := fmd.IsImageMode()

				Expect(err).NotTo(HaveOccurred())
				Expect(imageMode).To(BeFalse())
			})
		})
	})

	Describe("IsPackageMode", func() {
		Context("when system is in image mode", func() {
			It("should return false for package mode", func() {
				imageModeFs := afero.NewMemMapFs()
				imageModeFs.Create("/host-run/ostree-booted")

				fmd := utils.NewFilesystemModeDetectorWithFs(imageModeFs)
				packageMode, err := fmd.IsPackageMode()

				Expect(err).NotTo(HaveOccurred())
				Expect(packageMode).To(BeFalse())
			})
		})

		Context("when system is not in image mode", func() {
			It("should return true for package mode", func() {
				packageModeFs := afero.NewMemMapFs()

				fmd := utils.NewFilesystemModeDetectorWithFs(packageModeFs)
				packageMode, err := fmd.IsPackageMode()

				Expect(err).NotTo(HaveOccurred())
				Expect(packageMode).To(BeTrue())
			})
		})
	})

	Describe("DetectMode", func() {
		Context("when system is in image mode", func() {
			It("should return ImageMode", func() {
				imageModeFs := afero.NewMemMapFs()
				imageModeFs.Create("/host-run/ostree-booted")

				fmd := utils.NewFilesystemModeDetectorWithFs(imageModeFs)
				mode, err := fmd.DetectMode()

				Expect(err).NotTo(HaveOccurred())
				Expect(mode).To(Equal(utils.ImageMode))
			})
		})

		Context("when system is in package mode", func() {
			It("should return PackageMode", func() {
				packageModeFs := afero.NewMemMapFs()

				fmd := utils.NewFilesystemModeDetectorWithFs(packageModeFs)
				mode, err := fmd.DetectMode()

				Expect(err).NotTo(HaveOccurred())
				Expect(mode).To(Equal(utils.PackageMode))
			})
		})
	})

	Describe("Integration tests", func() {
		Context("when system has image mode characteristics", func() {
			It("should detect image mode correctly and package mode as false", func() {
				imageModeFs := afero.NewMemMapFs()
				imageModeFs.Create("/run/ostree-booted")

				fmd := utils.NewFilesystemModeDetectorWithFs(imageModeFs)

				imageMode, err := fmd.IsImageMode()
				Expect(err).NotTo(HaveOccurred())
				Expect(imageMode).To(BeTrue())

				packageMode, err := fmd.IsPackageMode()
				Expect(err).NotTo(HaveOccurred())
				Expect(packageMode).To(BeFalse())
			})
		})

		Context("when system has package mode characteristics", func() {
			It("should detect package mode correctly and image mode as false", func() {
				packageModeFs := afero.NewMemMapFs()

				fmd := utils.NewFilesystemModeDetectorWithFs(packageModeFs)

				imageMode, err := fmd.IsImageMode()
				Expect(err).NotTo(HaveOccurred())
				Expect(imageMode).To(BeFalse())

				packageMode, err := fmd.IsPackageMode()
				Expect(err).NotTo(HaveOccurred())
				Expect(packageMode).To(BeTrue())
			})
		})
	})
})
