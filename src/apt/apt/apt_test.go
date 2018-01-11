package apt_test

import (
	"apt/apt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/cloudfoundry/libbuildpack"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

//go:generate mockgen -source=apt.go --destination=mocks_test.go --package=apt_test

var _ = Describe("Apt", func() {
	var (
		a           *apt.Apt
		aptfile     string
		mockCtrl    *gomock.Controller
		mockCommand *MockCommand
		cacheDir    string
		installDir  string
	)
	BeforeEach(func() {
		aptfileHandle, err := ioutil.TempFile("", "aptfile.yml")
		Expect(err).ToNot(HaveOccurred())
		Expect(aptfileHandle.Close()).To(Succeed())
		aptfile = aptfileHandle.Name()

		cacheDir, _ = ioutil.TempDir("", "cachedir")
		installDir, _ = ioutil.TempDir("", "installdir")

		mockCtrl = gomock.NewController(GinkgoT())
		mockCommand = NewMockCommand(mockCtrl)
	})
	JustBeforeEach(func() {
		a = apt.New(mockCommand, aptfile, cacheDir, installDir)
	})
	AfterEach(func() {
		os.Remove(aptfile)
		os.RemoveAll(cacheDir)
		mockCtrl.Finish()
	})

	Describe("Setup", func() {
		JustBeforeEach(func() {
			Expect(libbuildpack.NewYAML().Write(aptfile, map[string][]string{
				"keys":     []string{"https://example.com/public.key"},
				"repos":    []string{"deb http://apt.example.com stable main"},
				"packages": []string{"abc", "def"},
			})).To(Succeed())
			Expect(a.Setup()).To(Succeed())
		})
		It("sets keys from apt.yml", func() {
			Expect(a.Keys).To(Equal([]string{"https://example.com/public.key"}))
		})
		It("sets repos from apt.yml", func() {
			Expect(a.Repos).To(Equal([]string{"deb http://apt.example.com stable main"}))
		})
		It("sets packages from apt.yml", func() {
			Expect(a.Packages).To(Equal([]string{"abc", "def"}))
		})
		It("copies sources.list", func() {
			Expect(filepath.Join(cacheDir, "apt", "sources", "sources.list")).To(BeARegularFile())
		})
		It("copies trusted.gpg", func() {
			Expect(filepath.Join(cacheDir, "apt", "etc", "trusted.gpg")).To(BeARegularFile())
		})
	})

	Describe("AddKeys", func() {
		Context("Keys have been specified", func() {
			JustBeforeEach(func() {
				a.Keys = []string{"https://example.com/public.key"}
			})
			It("adds the keys to the apt trusted keys", func() {
				mockCommand.EXPECT().Output(
					"/", "apt-key",
					"--keyring", filepath.Join(cacheDir, "apt", "etc", "trusted.gpg"),
					"adv",
					"--fetch-keys", "https://example.com/public.key",
				).Return("Shell output", nil)

				_, err := a.AddKeys()
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("No keys specified", func() {
			JustBeforeEach(func() {
				a.Keys = []string{}
			})
			It("does nothing", func() {
				_, err := a.AddKeys()
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Describe("AddRepos", func() {
		Context("Keys have been specified", func() {
			JustBeforeEach(func() {
				a.Repos = []string{"repo 11", "repo 12"}
			})
			It("adds the repos to the apt sources list", func() {
				sourceList := filepath.Join(cacheDir, "apt", "sources", "sources.list")
				Expect(os.MkdirAll(filepath.Dir(sourceList), 0755)).To(Succeed())
				Expect(ioutil.WriteFile(sourceList, []byte("repo 1\nrepo 2"), 0644)).To(Succeed())

				Expect(a.AddRepos()).To(Succeed())

				content, err := ioutil.ReadFile(sourceList)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(content)).To(Equal("repo 1\nrepo 2\nrepo 11\nrepo 12"))
			})
		})

		Context("No keys specified", func() {
			JustBeforeEach(func() {
				a.Keys = []string{}
			})
			It("does nothing", func() {
				_, err := a.AddKeys()
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Describe("Update", func() {
		It("runs apt update with expected options", func() {
			mockCommand.EXPECT().Output(
				"/", "apt-get",
				"-o", "debug::nolocking=true",
				"-o", "dir::cache="+cacheDir+"/apt/cache",
				"-o", "dir::state="+cacheDir+"/apt/state",
				"-o", "dir::etc::sourcelist="+cacheDir+"/apt/sources/sources.list",
				"-o", "dir::etc::trusted="+cacheDir+"/apt/etc/trusted.gpg",
				"update",
			).Return("Shell output", nil)

			output, err := a.Update()
			Expect(err).ToNot(HaveOccurred())
			Expect(output).To(Equal("Shell output"))
		})
	})

	Describe("Download", func() {
		JustBeforeEach(func() {
			a.Packages = []string{"http://example.com/holiday.deb", "disneyland"}
		})
		It("downloads user specified packages", func() {
			packageFile := cacheDir + "/apt/cache/archives/holiday.deb"
			mockCommand.EXPECT().Output(
				"/", "curl", "-s", "-L",
				"-z", packageFile,
				"-o", packageFile,
				"http://example.com/holiday.deb",
			).Return("curl output", nil)
			mockCommand.EXPECT().Output(
				"/", "apt-get",
				"-o", "debug::nolocking=true",
				"-o", "dir::cache="+cacheDir+"/apt/cache",
				"-o", "dir::state="+cacheDir+"/apt/state",
				"-o", "dir::etc::sourcelist="+cacheDir+"/apt/sources/sources.list",
				"-o", "dir::etc::trusted="+cacheDir+"/apt/etc/trusted.gpg",
				"-y", "--force-yes", "-d", "install", "--reinstall",
				"disneyland",
			).Return("apt output", nil)
			Expect(a.Download()).To(Equal(""))
		})
	})

	Describe("Install", func() {
		BeforeEach(func() {
			var err error
			cacheDir, err = ioutil.TempDir("", "cachedir")
			Expect(err).ToNot(HaveOccurred())
			Expect(os.MkdirAll(filepath.Join(cacheDir, "apt", "cache", "archives"), 0755)).To(Succeed())
			Expect(ioutil.WriteFile(filepath.Join(cacheDir, "apt", "cache", "archives", "holiday.deb"), []byte{}, 0644)).To(Succeed())
			Expect(ioutil.WriteFile(filepath.Join(cacheDir, "apt", "cache", "archives", "disneyland.deb"), []byte{}, 0644)).To(Succeed())
		})
		It("installs the downloaded debs", func() {
			mockCommand.EXPECT().Output("/", "dpkg", "-x", filepath.Join(cacheDir, "apt", "cache", "archives", "holiday.deb"), installDir)
			mockCommand.EXPECT().Output("/", "dpkg", "-x", filepath.Join(cacheDir, "apt", "cache", "archives", "disneyland.deb"), installDir)
			Expect(a.Install()).To(Equal(""))
		})
	})
})
