package images

import (
	"fmt"
	"time"

	g "github.com/onsi/ginkgo"
	o "github.com/onsi/gomega"

	exutil "github.com/openshift/origin/test/extended/util"
	kapi "k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/util/rand"
)

var _ = g.Describe("images: MySQL replication template", func() {
	defer g.GinkgoRecover()
	var (
		persistentVolumes []*kapi.PersistentVolume
		templatePath      = "https://raw.githubusercontent.com/openshift/mysql/master/5.5/examples/replica/mysql_replica.json"
		oc                = exutil.NewCLI("mysql-replication", exutil.KubeConfigPath())
		user              = "user"
		userPass          = "userpass"
		rootPass          = "rootpass"
		database          = "userdb"
	)

	g.Describe("MySQL replication", func() {
		g.JustBeforeEach(func() {
			var err error
			persistentVolumes, err = exutil.SetupHostPathVolumes(oc.AdminKubeREST().PersistentVolumes(), "mysql-replication", "512Mi", 1)
			o.Expect(err).NotTo(o.HaveOccurred())
		})

		g.AfterEach(func() {
			err := oc.AsAdmin().Run("delete").Args("all", "--all", "-n", oc.Namespace()).Execute()
			o.Expect(err).NotTo(o.HaveOccurred())

			err = oc.AsAdmin().Run("delete").Args("pvc", "--all", "-n", oc.Namespace()).Execute()
			o.Expect(err).NotTo(o.HaveOccurred())

			err = exutil.CleanupHostPathVolumes(oc.AdminKubeREST().PersistentVolumes(), "mysql-replication")
			o.Expect(err).NotTo(o.HaveOccurred())
		})

		g.It("should replicate data", func() {
			oc.SetOutputDir(exutil.TestContext.OutputDir)

			g.By(fmt.Sprintf("processing and creating the template %q", templatePath))
			configFile, err := oc.Run("process").
				Args(
				"-f", templatePath,
				"-v", "MYSQL_USER="+user+",MYSQL_PASSWORD="+userPass+",MYSQL_ROOT_PASSWORD="+rootPass+",MYSQL_DATABASE="+database).
				OutputToFile("config.json")
			o.Expect(err).NotTo(o.HaveOccurred())
			err = oc.Run("create").Args("-f", configFile).Execute()
			o.Expect(err).NotTo(o.HaveOccurred())

			createHelpers := func(masterDeployment, slaveDeployment string, slaveCount int) (*MySQL, []*MySQL) {

				/*
					// Wait for endpoints
					err := oc.KubeFramework().WaitForAnEndpoint("mysql-master")
					o.Expect(err).NotTo(o.HaveOccurred())
					err = oc.KubeFramework().WaitForAnEndpoint("mysql-slave")
					o.Expect(err).NotTo(o.HaveOccurred())
				*/

				podNames, err := exutil.WaitForPods(oc.KubeREST().Pods(oc.Namespace()), exutil.ParseLabelsOrDie(fmt.Sprintf("deployment=%s", masterDeployment)), 1, 60*time.Second)
				o.Expect(err).NotTo(o.HaveOccurred())
				masterPod := podNames[0]

				slavePods, err := exutil.WaitForPods(oc.KubeREST().Pods(oc.Namespace()), exutil.ParseLabelsOrDie(fmt.Sprintf("deployment=%s", slaveDeployment)), slaveCount, 120*time.Second)
				o.Expect(err).NotTo(o.HaveOccurred())

				// Create MySQL helper for master
				master, err := NewMysql(oc, masterPod, "")
				o.Expect(err).NotTo(o.HaveOccurred())
				o.Expect(master.WaitUntilUp(120 * time.Second)).NotTo(o.HaveOccurred())

				// Create MySQL helpers for slaves
				slaves := make([]*MySQL, len(slavePods))
				for i := range slavePods {
					slave, err := NewMysql(oc, slavePods[i], masterPod)
					o.Expect(err).NotTo(o.HaveOccurred())
					o.Expect(slave.WaitUntilUp(120 * time.Second)).NotTo(o.HaveOccurred())
					slaves[i] = slave
				}

				return master, slaves
			}

			assertReplicationIsWorking := func(master *MySQL, slaves []*MySQL) {
				table := fmt.Sprintf("table_%s", rand.String(10))

				// Test if we can query as root
				_, err = master.QueryAsRoot("SELECT 1;")
				o.Expect(err).NotTo(o.HaveOccurred())

				// Create a new table with random name
				_, err = master.QueryAsUser(fmt.Sprintf("CREATE TABLE %s (col1 VARCHAR(20), col2 VARCHAR(20));", table))
				o.Expect(err).NotTo(o.HaveOccurred())

				// Write new data to the table through master
				_, err = master.QueryAsUser(fmt.Sprintf("INSERT INTO %s (col1, col2) VALUES ('val1', 'val2');", table))
				o.Expect(err).NotTo(o.HaveOccurred())

				// Make sure data is present on master
				err = master.WaitForMySQLOutput(10*time.Second, false, fmt.Sprintf("SELECT * FROM %s\\G;", table), "col1: val1\ncol2: val2")
				o.Expect(err).NotTo(o.HaveOccurred())

				// Make sure data was replicated on all slaves
				for _, slave := range slaves {
					err = slave.WaitForMySQLOutput(60*time.Second, false, fmt.Sprintf("SELECT * FROM %s\\G;", table), "col1: val1\ncol2: val2")
					o.Expect(err).NotTo(o.HaveOccurred())
				}
			}

			g.By("after initial deployment")
			master, slaves := createHelpers("mysql-master-1", "mysql-slave-1", 1)
			assertReplicationIsWorking(master, slaves)

			g.By("after master is restarted by changing the Deployment Config")
			err = oc.Run("env").Args("dc", "mysql-master", "MYSQL_ROOT_PASSWORD=newpass").Execute()
			o.Expect(err).NotTo(o.HaveOccurred())
			master, slaves = createHelpers("mysql-master-2", "mysql-slave-1", 1)
			assertReplicationIsWorking(master, slaves)

			g.By("after master is restarted by deleting the pod")
			err = oc.Run("delete").Args("pod", "-l", "deployment=mysql-master-2").Execute()
			o.Expect(err).NotTo(o.HaveOccurred())
			master, slaves = createHelpers("mysql-master-2", "mysql-slave-1", 1)
			assertReplicationIsWorking(master, slaves)

			g.By("after slave is restarted by deleting the pod")
			err = oc.Run("delete").Args("pod", "-l", "deployment=mysql-slave-1").Execute()
			o.Expect(err).NotTo(o.HaveOccurred())
			master, slaves = createHelpers("mysql-master-2", "mysql-slave-1", 1)
			assertReplicationIsWorking(master, slaves)

			g.By("after slave is scaled to 0 and then back to 4 replicas")
			err = oc.Run("scale").Args("dc", "mysql-slave", "--replicas=0").Execute()
			o.Expect(err).NotTo(o.HaveOccurred())
			err = oc.Run("scale").Args("dc", "mysql-slave", "--replicas=4").Execute()
			o.Expect(err).NotTo(o.HaveOccurred())
			master, slaves = createHelpers("mysql-master-2", "mysql-slave-1", 4)
			assertReplicationIsWorking(master, slaves)
		})
	})
})
