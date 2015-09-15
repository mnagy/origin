package images

import (
	"fmt"
	"time"

	g "github.com/onsi/ginkgo"
	o "github.com/onsi/gomega"

	exutil "github.com/openshift/origin/test/extended/util"
	kapi "k8s.io/kubernetes/pkg/api"
)

var _ = g.Describe("images: MySQL persistent template", func() {
	defer g.GinkgoRecover()
	var (
		persistentVolumes []*kapi.PersistentVolume
		templatePath      = exutil.FixturePath("..", "..", "examples", "db-templates", "mysql-persistent-template.json")
		oc                = exutil.NewCLI("mysql-create", exutil.KubeConfigPath())
	)
	g.JustBeforeEach(func() {
		var err error
		persistentVolumes, err = exutil.SetupHostPathVolumes(oc.AdminKubeREST().PersistentVolumes(), "mysql-persistent", "512Mi", 1)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.AfterEach(func() {
		err := oc.AsAdmin().Run("delete").Args("all", "--all", "-n", oc.Namespace()).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		err = oc.AsAdmin().Run("delete").Args("pvc", "--all", "-n", oc.Namespace()).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		err = exutil.CleanupHostPathVolumes(oc.AdminKubeREST().PersistentVolumes(), "mysql-persistent")
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.Describe("Creating from a template", func() {
		g.It(fmt.Sprintf("should process and create the %q template", templatePath), func() {
			oc.SetOutputDir(exutil.TestContext.OutputDir)

			g.By(fmt.Sprintf("calling oc process -f %q", templatePath))
			configFile, err := oc.Run("process").Args("-f", templatePath).OutputToFile("config.json")
			o.Expect(err).NotTo(o.HaveOccurred())

			g.By(fmt.Sprintf("calling oc create -f %q", configFile))
			err = oc.Run("create").Args("-f", configFile).Execute()
			o.Expect(err).NotTo(o.HaveOccurred())

			g.By("expecting the mysql service get endpoints")
			err = oc.KubeFramework().WaitForAnEndpoint("mysql")
			o.Expect(err).NotTo(o.HaveOccurred())

			g.By("changing user and root passwords")
			err = oc.Run("env").Args("dc", "mysql", "MYSQL_PASSWORD=userpass", "MYSQL_ROOT_PASSWORD=rootpass").Execute()
			o.Expect(err).NotTo(o.HaveOccurred())

			podNames, err := exutil.WaitForPods(oc.KubeREST().Pods(oc.Namespace()), exutil.ParseLabelsOrDie("deployment=mysql-2"), 1, 60*time.Second)
			o.Expect(err).NotTo(o.HaveOccurred())

			mysql, err := NewMysql(oc, podNames[0], "")
			o.Expect(err).NotTo(o.HaveOccurred())
			err = mysql.WaitUntilUp(30 * time.Second)
			o.Expect(err).NotTo(o.HaveOccurred())

			_, err = mysql.QueryAsUser("SELECT 1;")
			o.Expect(err).NotTo(o.HaveOccurred())
			_, err = mysql.QueryAsRoot("SELECT 1;")
			o.Expect(err).NotTo(o.HaveOccurred())
		})
	})

})
