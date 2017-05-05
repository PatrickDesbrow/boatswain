// Copyright © 2017 NAME HERE <EMAIL ADDRESS>
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
	"io"
	"os/exec"
	"strings"

	"fmt"

	utils "github.com/medbridge/boatswain/utilities"
	"github.com/spf13/cobra"
)

// provisionCmd represents the provision command
var provisionCmd = &cobra.Command{
	Use:   "provision",
	Short: "Provisions a new AWS k8s cluster",
	Long: `
  
  Attempts to provision the following whether or not it's been done already (i.e. it's kinda dumb).
  That being said, it's an idempotent command and you shouldn't run into any trouble accidently running it
  on an already provisioned cluster (UNLESS THAT CLUSTER IS TAKING PRODUCTION TRAFFIC).

  * swaps in modified spc-balancer to work with calico networking
  * annotates default namespace to deny external traffic by default 
  * labels default and stackpoint-system namespaces with respective networkpolicy names
  * installs Elasticsearch, Logstash, Kibana (ELK) stack

  IMPORTANT: Don't run this in a cluster taking production traffic.
	`,
	Run: func(cmd *cobra.Command, args []string) {

		utils.EchoWarningMessage("Make sure this cluster is not taking production traffic")
		fmt.Println("")

		utils.DisplayK8sCurrContext()
		currContext := getCurrContext()

		msg := "Are you sure you want to run provision in " + currContext + " context?"
		resp := utils.AskForConfirmation(msg)
		if resp == true {
			deleteOldSPCBalancer()
			createSPCBalancer()
			annotateNS()
			labelDefaultNS()
			labelSPCNS()
			releaseELKStack()
		}

	},
}

func init() {
	RootCmd.AddCommand(provisionCmd)
}

func deleteOldSPCBalancer() {
	cmdArgs := []string{"delete", "rs", "spc-balancer", "-n", "stackpoint-system"}
	execKubectlCmd(cmdArgs)
}

func createSPCBalancer() {
	cmdArgs := []string{"create", "-f", "-"}
	cmd := exec.Command("kubectl", cmdArgs...)
	stdin, _ := cmd.StdinPipe()
	io.WriteString(stdin, spcBalancer)
	stdin.Close()
	cmd.CombinedOutput()

	fmt.Println(cmd.Stdout)
}

func annotateNS() {
	cmdArgs := []string{"annotate", "ns", "default", "net.beta.kubernetes.io/network-policy={\"ingress\": {\"isolation\": \"DefaultDeny\""}
	execKubectlCmd(cmdArgs)
}

func labelDefaultNS() {
	cmdArgs := []string{"label", "ns", "default", "networkpolicy_name=medbridge"}
	execKubectlCmd(cmdArgs)
}

func labelSPCNS() {
	cmdArgs := []string{"label", "ns", "stackpoint-system", "networkpolicy_name=stackpoint-system"}
	execKubectlCmd(cmdArgs)
}

func execKubectlCmd(cmdArgs []string) {
	cmd := "kubectl"
	out, err := exec.Command(cmd, cmdArgs...).CombinedOutput()
	if err != nil {
		fmt.Printf("There was an error: %s", err)
	}
	fmt.Printf("%s", out)
}

func releaseELKStack() {
	currContext := getCurrContext()
	cmd := "boatswain"
	cmdArgs := []string{"release", "elk", "-e", currContext}
	out, err := exec.Command(cmd, cmdArgs...).CombinedOutput()
	if err != nil {
		fmt.Printf("There was an error: %s", err)
	}
	fmt.Printf("%s", out)
}

func getCurrContext() string {
	args := []string{"config", "current-context"}
	currContext, err := exec.Command("kubectl", args...).CombinedOutput()
	if err != nil {
		panic(err)
	}
	currContextStr := string(currContext[:])
	currContextStr = strings.TrimRight(currContextStr, "\r\n")
	return currContextStr
}

var spcBalancer = `
apiVersion: v1
data:
  full-template-balancer: |
    {{ with .Global}}
    global
      daemon
      pidfile /var/run/haproxy.pid
      stats socket /var/run/haproxy.stat mode 777
      maxconn {{ .Maxconn }}
      maxpipes {{ .Maxpipes }}
      spread-checks {{ .SpreadChecks }}{{ if .Debug }}
      debug{{ end }}{{ end }}


    {{ with .Defaults }}
    defaults
      log global
      mode {{ .Mode }}
      balance {{ .Balance }}
      maxconn {{ .Maxconn }}
      {{ if .TCPLog }}option tcplog{{ end }}
      {{ if .HTTPLog }}option httplog{{ end }}
      {{ if .AbortOnClose }}option abortonclose{{ end }}
      {{ if .HTTPServerClose }}option httpclose{{ end }}
      retries {{ .Retries }}
      {{ if .Redispatch }}option redispatch{{ end }}
      timeout client {{ .TimeoutClient }}
      timeout connect {{ .TimeoutConnect }}
      timeout server {{ .TimeoutServer }}
      {{ if .DontLogNull }}option dontlognull{{ end }}
      timeout check {{ .TimeoutCheck }}
    {{ end }}{{$certs_dir:= .CertsDir }}{{ range .Frontends }}

    frontend {{ .Name }}{{ with .Bind }}
      bind {{ .IP }}:{{ .Port }}{{ if .IsTLS }} ssl {{ range .Certs }}crt {{$certs_dir}}/{{.Name}}.pem {{ end }}{{ end }}{{ end }}{{ if .DefaultBackend.Backend }}
      default_backend {{ .DefaultBackend.Backend }}{{end}}
      http-request replace-value Host (.*):.* \1 if { hdr_sub(Host) : }{{ range .ACLs }}
      acl {{ .Name }} {{.Content}}{{end}}{{ range .UseBackendsByPrio }}
      use_backend {{ .Backend }} if {{ range .ACLs }}{{ .Name }} {{end}}{{end}}
    {{ end }}
    {{range $name, $be := .Backends}}
    backend {{$name}}{{ range $sname, $server := .Servers}}
      server {{ $sname }} {{ $server.Address }}:{{ $server.Port }} check inter {{ $server.CheckInter}}{{end}}
    {{end}}
kind: ConfigMap
metadata:
  creationTimestamp: null
  name: haproxy-config
  selfLink: /api/v1/namespaces//configmaps/haproxy-config
  namespace: stackpoint-system
---
apiVersion: extensions/v1beta1
kind: ReplicaSet
metadata:
  namespace: stackpoint-system
  annotations:
    deployment.kubernetes.io/desired-replicas: "1"
    deployment.kubernetes.io/max-replicas: "2"
    deployment.kubernetes.io/revision: "1"
  creationTimestamp: null
  generation: 1
  labels:
    app: spc-balancer
  name: spc-balancer
spec:
  replicas: 1
  selector:
    matchLabels:
      app: spc-balancer
  template:
    metadata:
      creationTimestamp: null
      labels:
        app: spc-balancer
        networkpolicy_ingress: controller
    spec:
      containers:
      - env:
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.namespace
        - name: BALANCER_IP
          value: 0.0.0.0
        - name: BALANCER_API_PORT
          value: "8207"
        image: quay.io/stackpoint/haproxy-ingress-controller:0.1.4
        imagePullPolicy: Always
        livenessProbe:
          failureThreshold: 3
          httpGet:
            path: /healthz
            port: 8207
            scheme: HTTP
          initialDelaySeconds: 30
          periodSeconds: 10
          successThreshold: 1
          timeoutSeconds: 5
        name: spc-balancer
        ports:
        - containerPort: 80
          name: http
          protocol: TCP
        - containerPort: 443
          name: https
          protocol: TCP
        resources: {}
        terminationMessagePath: /dev/termination-log
      dnsPolicy: ClusterFirst
      restartPolicy: Always
      securityContext: {}
      terminationGracePeriodSeconds: 30
status:
  replicas: 0
---
apiVersion: v1
kind: Service
metadata:
  creationTimestamp: null
  name: spc-balancer
  selfLink: /api/v1/namespaces//services/spc-balancer
  namespace: stackpoint-system
spec:
  ports:
  - name: http
    port: 80
    protocol: TCP
    targetPort: 80
    nodePort: 30080
  - name: https
    port: 443
    protocol: TCP
    targetPort: 443
    nodePort: 30443
  selector:
    app: spc-balancer
  sessionAffinity: None
  type: NodePort
status:
  loadBalancer: {}`
