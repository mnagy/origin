package images

import (
	"fmt"
	"strings"
	"time"

	exutil "github.com/openshift/origin/test/extended/util"
	"k8s.io/kubernetes/pkg/util/wait"
)

// MySQL is a MySQL helper for executing commands
type MySQL struct {
	oc        *exutil.CLI
	PodName   string
	Container string
	Env       map[string]string
}

// NewMysql queries OpenShift for a pod with given name, saving environment
// variables like username and password for easier use.
func NewMysql(oc *exutil.CLI, podName, masterPodName string) (*MySQL, error) {
	m := &MySQL{
		oc:      oc,
		PodName: podName,
	}

	if pod, err := oc.KubeREST().Pods(oc.Namespace()).Get(podName); err != nil {
		return nil, err
	} else {
		m.Container = pod.Spec.Containers[0].Name
	}

	if masterPodName != "" {
		// Some values are replicated to the master, we'll need to get them
		if err := m.GetEnv(masterPodName); err != nil {
			return nil, err
		}
	}
	if err := m.GetEnv(podName); err != nil {
		return nil, err
	}
	return m, nil
}

// GetEnv will get and save environment variables from a pod
func (m *MySQL) GetEnv(podName string) error {
	pod, err := m.oc.KubeREST().Pods(m.oc.Namespace()).Get(podName)
	if err != nil {
		return err
	}
	if m.Env == nil {
		m.Env = make(map[string]string)
	}
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			m.Env[env.Name] = env.Value
		}
	}
	return nil
}

// WaitForMySQL pings the MySQL server
func (m *MySQL) WaitForMySQL() (bool, error) {
	out, err := m.oc.ExecWithoutExit(m.PodName, m.Container, "mysqladmin -h 127.0.0.1 -uroot ping").Output()
	if err != nil {
		switch err.(type) {
		case *exutil.ExitError:
			return false, nil
		default:
			return false, err
		}
	}
	return out == "mysqld is alive", nil
}

// WaitUntilUp continuously waits for the server to respond to pings, up until timeout.
func (m *MySQL) WaitUntilUp(timeout time.Duration) error {
	return wait.Poll(2*time.Second, timeout, m.WaitForMySQL)
}

// QueryAsUser executes an SQL query as a root user and returns the result.
func (m *MySQL) QueryAsRoot(sql string) (string, error) {
	return m.oc.ExecWithoutExit(m.PodName, m.Container, fmt.Sprintf("mysql -h 127.0.0.1 -uroot -e \"%s\" %s", sql, m.Env["MYSQL_DATABASE"])).Output()
}

// QueryAsUser executes an SQL query as an ordinary user and returns the result.
func (m *MySQL) QueryAsUser(sql string) (string, error) {
	return m.oc.ExecWithoutExit(m.PodName, m.Container, fmt.Sprintf("mysql -h 127.0.0.1 -u%s -p%s -e \"%s\" %s", m.Env["MYSQL_USER"], m.Env["MYSQL_PASSWORD"], sql, m.Env["MYSQL_DATABASE"])).Output()
}

// WaitForMySQLOutput will execute the SQL query multiple times, until the
// specified substring is found in the results. This function should be used for
// testing replication, since it might take some time untill the data is propagated
// to slaves.
func (m *MySQL) WaitForMySQLOutput(timeout time.Duration, root bool, sql, resultSubstr string) error {
	return wait.Poll(5*time.Second, timeout, func() (bool, error) {
		var (
			out string
			err error
		)

		if root {
			out, err = m.QueryAsRoot(sql)
		} else {
			out, err = m.QueryAsUser(sql)
		}
		if _, ok := err.(*exutil.ExitError); ok {
			// Ignore exit errors from mysql (e.g. table doesn't exist)
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if strings.Contains(out, resultSubstr) {
			return true, nil
		}
		return false, nil
	})
}
