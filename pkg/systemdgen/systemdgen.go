package systemdgen

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"text/template"
	"time"

	"github.com/containers/libpod/version"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// ContainerInfo contains data required for generating a container's systemd
// unit file.
type ContainerInfo struct {
	// ServiceName of the systemd service.
	ServiceName string
	// Name or ID of the container.
	ContainerName string
	// InfraContainer of the pod.
	InfraContainer string
	// StopTimeout sets the timeout Podman waits before killing the container
	// during service stop.
	StopTimeout int
	// RestartPolicy of the systemd unit (e.g., no, on-failure, always).
	RestartPolicy string
	// PIDFile of the service. Required for forking services. Must point to the
	// PID of the associated conmon process.
	PIDFile string
	// GenerateTimestamp, if set the generated unit file has a time stamp.
	GenerateTimestamp bool
	// BoundToServices are the services this service binds to.  Note that this
	// service runs after them.
	BoundToServices []string
	// RequiredServices are services this service requires. Note that this
	// service runs before them.
	RequiredServices []string
	// PodmanVersion for the header. Will be set internally. Will be auto-filled
	// if left empty.
	PodmanVersion string
	// Executable is the path to the podman executable. Will be auto-filled if
	// left empty.
	Executable string
	// TimeStamp at the time of creating the unit file. Will be set internally.
	TimeStamp string
}

var restartPolicies = []string{"no", "on-success", "on-failure", "on-abnormal", "on-watchdog", "on-abort", "always"}

// validateRestartPolicy checks that the user-provided policy is valid.
func validateRestartPolicy(restart string) error {
	for _, i := range restartPolicies {
		if i == restart {
			return nil
		}
	}
	return errors.Errorf("%s is not a valid restart policy", restart)
}

const containerTemplate = `# {{.ServiceName}}.service
# autogenerated by Podman {{.PodmanVersion}}
{{- if .TimeStamp}}
# {{.TimeStamp}}
{{- end}}

[Unit]
Description=Podman {{.ServiceName}}.service
Documentation=man:podman-generate-systemd(1)
{{- if .BoundToServices}}
RefuseManualStart=yes
RefuseManualStop=yes
BindsTo={{- range $index, $value := .BoundToServices -}}{{if $index}} {{end}}{{ $value }}.service{{end}}
After={{- range $index, $value := .BoundToServices -}}{{if $index}} {{end}}{{ $value }}.service{{end}}
{{- end}}
{{- if .RequiredServices}}
Requires={{- range $index, $value := .RequiredServices -}}{{if $index}} {{end}}{{ $value }}.service{{end}}
Before={{- range $index, $value := .RequiredServices -}}{{if $index}} {{end}}{{ $value }}.service{{end}}
{{- end}}

[Service]
Restart={{.RestartPolicy}}
ExecStart={{.Executable}} start {{.ContainerName}}
ExecStop={{.Executable}} stop {{if (ge .StopTimeout 0)}}-t {{.StopTimeout}}{{end}} {{.ContainerName}}
KillMode=none
Type=forking
PIDFile={{.PIDFile}}

[Install]
WantedBy=multi-user.target`

// CreateContainerSystemdUnit creates a systemd unit file for a container.
func CreateContainerSystemdUnit(info *ContainerInfo, generateFiles bool) (string, error) {
	if err := validateRestartPolicy(info.RestartPolicy); err != nil {
		return "", err
	}

	// Make sure the executable is set.
	if info.Executable == "" {
		executable, err := os.Executable()
		if err != nil {
			executable = "/usr/bin/podman"
			logrus.Warnf("Could not obtain podman executable location, using default %s", executable)
		}
		info.Executable = executable
	}

	if info.PodmanVersion == "" {
		info.PodmanVersion = version.Version
	}
	if info.GenerateTimestamp {
		info.TimeStamp = fmt.Sprintf("%v", time.Now().Format(time.UnixDate))
	}

	// Sort the slices to assure a deterministic output.
	sort.Strings(info.RequiredServices)
	sort.Strings(info.BoundToServices)

	// Generate the template and compile it.
	templ, err := template.New("systemd_service_file").Parse(containerTemplate)
	if err != nil {
		return "", errors.Wrap(err, "error parsing systemd service template")
	}

	var buf bytes.Buffer
	if err := templ.Execute(&buf, info); err != nil {
		return "", err
	}

	if !generateFiles {
		return buf.String(), nil
	}

	buf.WriteByte('\n')
	cwd, err := os.Getwd()
	if err != nil {
		return "", errors.Wrap(err, "error getting current working directory")
	}
	path := filepath.Join(cwd, fmt.Sprintf("%s.service", info.ServiceName))
	if err := ioutil.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return "", errors.Wrap(err, "error generating systemd unit")
	}
	return path, nil
}
