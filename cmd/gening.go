// Copyright © 2017 NAME HERE eric@medbridgeed.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"text/template"

	"github.com/spf13/cobra"
)

var service string
var servicePort string
var enableTLS bool

type Ingress struct {
	HostName    string
	ServiceName string
	ServicePort string
	SecretName  string
	EnableTLS   bool
}

type GenIngressFlags struct {
	Service     string
	ServicePort string
	EnableTLS   bool
}

type ICommandFactory interface {
	Command(string, ...string) ICommand
}

type ICommand interface {
	CombinedOutput() ([]byte, error)
	StdinPipe() (io.WriteCloser, error)
}

type TestCommand struct {
	Command []string
}

type TestStdInPipe struct {
	Closed bool
	Bytes  []byte
}

func (t *TestStdInPipe) Close() error {
	t.Closed = true
	return nil
}

func (t *TestStdInPipe) Write(p []byte) (int, error) {
	t.Bytes = p
	return 1, nil
}

func (e *TestCommand) CombinedOutput() ([]byte, error) {
	return nil, nil
}

func (e *TestCommand) StdinPipe() (io.WriteCloser, error) {
	var stub TestStdInPipe
	return &stub, nil
}

type CommandTestFactory struct {
	Commands [][]string
}

func (e *CommandTestFactory) Command(name string, arg ...string) ICommand {
	cmdString := []string{name}
	cmdString = append(cmdString, arg...)
	e.Commands = append(e.Commands, cmdString)
	cmd := TestCommand{Command: cmdString}
	return &cmd
}

type CommandFactory struct{}

func (e *CommandFactory) Command(name string, arg ...string) ICommand {
	return exec.Command(name, arg...)
}

// geningCmd represents the gening command
var geningCmd = &cobra.Command{
	Use:   "gening hostname",
	Short: "Generate ingress",
	Long: `Example:
	
	boatswain gening example.com`,
	Run: func(cmd *cobra.Command, args []string) {
		cmdFactory := &CommandFactory{}
		flags := GenIngressFlags{Service: service, ServicePort: servicePort, EnableTLS: enableTLS}
		RunGenIngress(args, cmdFactory, flags)
	},
}

// RunGenIngress command for boatswain gening
func RunGenIngress(args []string, cmdFactory ICommandFactory, cmdFlags GenIngressFlags) {
	if len(args) == 0 {
		fmt.Println("Missing argument: host")
	}
	host := args[0]
	ingress := Ingress{
		HostName:    host,
		ServiceName: cmdFlags.Service,
		ServicePort: cmdFlags.ServicePort,
		EnableTLS:   cmdFlags.EnableTLS}

	if cmdFlags.EnableTLS {
		secretName := "tls-" + host

		cmdName := "openssl"
		cmdArgs := []string{
			"req", "-x509",
			"-sha256", "-nodes", "-newkey", "rsa:4096",
			"-keyout", "tls.key",
			"-out", "tls.crt",
			"-days", "365",
			"-subj", "/CN=" + host}

		cmdFactory.Command(cmdName, cmdArgs...).CombinedOutput()

		cmdName = "kubectl"
		cmdArgs = []string{
			"create", "secret", "tls", secretName, "--cert=./tls.crt", "--key=./tls.key"}
		cmdFactory.Command(cmdName, cmdArgs...).CombinedOutput()

		os.Remove("tls.crt")
		os.Remove("tls.key")

		ingress.SecretName = secretName
	}

	k8smanifest, _ := getIngress(ingress)

	ingCmd := cmdFactory.Command("kubectl", "apply", "-f", "-")
	stdin, _ := ingCmd.StdinPipe()

	go func() {
		defer stdin.Close()
		io.WriteString(stdin, k8smanifest)

	}()

	ingCmd.CombinedOutput()
}

func init() {
	RootCmd.AddCommand(geningCmd)

	geningCmd.Flags().StringVarP(&service, "service", "s", "dev-medbridge", "Service name")
	geningCmd.Flags().StringVarP(&servicePort, "port", "p", "80", "Service port")
	geningCmd.Flags().BoolVarP(&enableTLS, "tls", "t", false, "Enable TLS")

}

func getIngress(ing Ingress) (string, error) {
	tmpl, err := template.New("ingress").Parse(`
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: {{.HostName}}
spec:
  rules:
  - host: {{.HostName}}
    http:
      paths:
      - backend:
          serviceName: {{.ServiceName}}
          servicePort: {{.ServicePort}}
{{ if .EnableTLS }}
  tls:
  - hosts:
    - {{.HostName}}
    secretName: {{.SecretName}}
{{ end }}
`)
	var doc bytes.Buffer
	tmpl.Execute(&doc, ing)
	err = tmpl.Execute(&doc, ing)
	s := doc.String()
	return s, err
}
