// Copyright 2016 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package swarm

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/docker/docker/api/types/swarm"
	"github.com/fsouza/go-dockerclient"
	"github.com/pkg/errors"
	"github.com/tsuru/config"
	"github.com/tsuru/tsuru/app/image"
	"github.com/tsuru/tsuru/event"
	tsuruNet "github.com/tsuru/tsuru/net"
	"github.com/tsuru/tsuru/provision"
	"github.com/tsuru/tsuru/provision/dockercommon"
)

const (
	provisionerName = "swarm"

	labelInternalPrefix = "tsuru-internal-"
	labelDockerAddr     = labelInternalPrefix + "docker-addr"
)

var errNotImplemented = errors.New("not implemented")

type swarmProvisioner struct{}

func init() {
	provision.Register(provisionerName, func() (provision.Provisioner, error) {
		return &swarmProvisioner{}, nil
	})
}

func (p *swarmProvisioner) Initialize() error {
	var err error
	swarmConfig.swarmPort, err = config.GetInt("swarm:swarm-port")
	if err != nil {
		swarmConfig.swarmPort = 2377
	}
	caPath, _ := config.GetString("swarm:tls:root-path")
	if caPath != "" {
		swarmConfig.tlsConfig, err = readTLSConfig(caPath)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *swarmProvisioner) GetName() string {
	return provisionerName
}

func (p *swarmProvisioner) Provision(provision.App) error {
	return nil
}

func (p *swarmProvisioner) Destroy(provision.App) error {
	return errNotImplemented
}

func (p *swarmProvisioner) AddUnits(provision.App, uint, string, io.Writer) ([]provision.Unit, error) {
	return nil, errNotImplemented
}

func (p *swarmProvisioner) RemoveUnits(provision.App, uint, string, io.Writer) error {
	return errNotImplemented
}

func (p *swarmProvisioner) SetUnitStatus(provision.Unit, provision.Status) error {
	return errNotImplemented
}

func (p *swarmProvisioner) Restart(provision.App, string, io.Writer) error {
	return errNotImplemented
}

func (p *swarmProvisioner) Start(provision.App, string) error {
	return errNotImplemented
}

func (p *swarmProvisioner) Stop(provision.App, string) error {
	return errNotImplemented
}

func (p *swarmProvisioner) Units(app provision.App) ([]provision.Unit, error) {
	client, err := chooseDBSwarmNode()
	if err != nil {
		return nil, err
	}
	tasks, err := client.ListTasks(docker.ListTasksOptions{
		Filters: map[string][]string{
			"label": {fmt.Sprintf("%s=%s", labelAppName, app.GetName())},
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "")
	}
	nodeMap := map[string]*swarm.Node{}
	serviceMap := map[string]*swarm.Service{}
	units := make([]provision.Unit, len(tasks))
	for i, t := range tasks {
		if _, ok := nodeMap[t.NodeID]; !ok {
			var node *swarm.Node
			node, err = client.InspectNode(t.NodeID)
			if err != nil {
				return nil, errors.Wrap(err, "")
			}
			nodeMap[t.NodeID] = node
		}
		if _, ok := serviceMap[t.ServiceID]; !ok {
			var service *swarm.Service
			service, err = client.InspectService(t.ServiceID)
			if err != nil {
				return nil, errors.Wrap(err, "")
			}
			serviceMap[t.ServiceID] = service
		}
		addr := nodeMap[t.NodeID].Spec.Labels[labelDockerAddr]
		service := serviceMap[t.ServiceID]
		host := tsuruNet.URLToHost(addr)
		var pubPort uint32
		if len(service.Endpoint.Ports) > 0 {
			pubPort = service.Endpoint.Ports[0].PublishedPort
		}
		units[i] = provision.Unit{
			ID:          t.Status.ContainerStatus.ContainerID,
			AppName:     app.GetName(),
			ProcessName: service.Spec.Annotations.Labels[labelAppProcess.String()],
			Type:        app.GetPlatform(),
			Ip:          host,
			Status:      provision.StatusStarted,
			Address: &url.URL{
				Scheme: "http",
				Host:   fmt.Sprintf("%s:%d", host, pubPort),
			},
		}
	}
	return units, nil
}

func (p *swarmProvisioner) RoutableUnits(app provision.App) ([]provision.Unit, error) {
	imgID, err := image.AppCurrentImageName(app.GetName())
	if err != nil && err != image.ErrNoImagesAvailable {
		return nil, err
	}
	webProcessName, err := image.GetImageWebProcessName(imgID)
	if err != nil {
		return nil, err
	}
	units, err := p.Units(app)
	if err != nil {
		return nil, err
	}
	for i := 0; i < len(units); i++ {
		if units[i].ProcessName != webProcessName {
			units = append(units[:i], units[i+1:]...)
			i--
		}
	}
	return units, nil
}

func (p *swarmProvisioner) RegisterUnit(unit provision.Unit, customData map[string]interface{}) error {
	if customData == nil {
		return nil
	}
	client, err := chooseDBSwarmNode()
	if err != nil {
		return err
	}
	tasks, err := client.ListTasks(docker.ListTasksOptions{
		Filters: map[string][]string{
			"label": {labelServiceDeploy.String() + "=true"},
		},
	})
	if err != nil {
		return errors.Wrap(err, "")
	}
	var foundTask *swarm.Task
	for i, t := range tasks {
		if t.Status.ContainerStatus.ContainerID == unit.ID {
			foundTask = &tasks[i]
			break
		}
	}
	if foundTask == nil {
		return nil
	}
	srv, err := client.InspectService(foundTask.ServiceID)
	if err != nil {
		return errors.Wrap(err, "")
	}
	buildingImage := srv.Spec.Annotations.Labels[labelServiceBuildImage.String()]
	if buildingImage == "" {
		return errors.Errorf("invalid build image label for build service: %#v", srv)
	}
	return image.SaveImageCustomData(buildingImage, customData)
}

func (p *swarmProvisioner) ListNodes(addressFilter []string) ([]provision.Node, error) {
	client, err := chooseDBSwarmNode()
	if err != nil {
		if errors.Cause(err) == errNoSwarmNode {
			return nil, nil
		}
		return nil, err
	}
	nodes, err := client.ListNodes(docker.ListNodesOptions{})
	if err != nil {
		return nil, err
	}
	var filterMap map[string]struct{}
	if len(addressFilter) > 0 {
		filterMap = map[string]struct{}{}
		for _, addr := range addressFilter {
			filterMap[tsuruNet.URLToHost(addr)] = struct{}{}
		}
	}
	nodeList := make([]provision.Node, 0, len(nodes))
	for i := range nodes {
		wrapped := &swarmNodeWrapper{Node: &nodes[i], provisioner: p}
		toAdd := true
		if filterMap != nil {
			_, toAdd = filterMap[tsuruNet.URLToHost(wrapped.Address())]
		}
		if toAdd {
			nodeList = append(nodeList, wrapped)
		}
	}
	return nodeList, nil
}

func (p *swarmProvisioner) GetNode(address string) (provision.Node, error) {
	nodes, err := p.ListNodes([]string{address})
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, provision.ErrNodeNotFound
	}
	return nodes[0], nil
}

func (p *swarmProvisioner) AddNode(opts provision.AddNodeOptions) error {
	existingClient, err := chooseDBSwarmNode()
	if err != nil && errors.Cause(err) != errNoSwarmNode {
		return err
	}
	newClient, err := newClient(opts.Address)
	if err != nil {
		return err
	}
	if existingClient == nil {
		err = initSwarm(newClient, opts.Address)
	} else {
		err = joinSwarm(existingClient, newClient, opts.Address)
	}
	if err != nil {
		return err
	}
	dockerInfo, err := newClient.Info()
	if err != nil {
		return errors.Wrap(err, "")
	}
	nodeData, err := newClient.InspectNode(dockerInfo.Swarm.NodeID)
	if err != nil {
		return errors.Wrap(err, "")
	}
	nodeData.Spec.Annotations.Labels = map[string]string{
		labelDockerAddr: opts.Address,
	}
	for k, v := range opts.Metadata {
		nodeData.Spec.Annotations.Labels[k] = v
	}
	err = newClient.UpdateNode(dockerInfo.Swarm.NodeID, docker.UpdateNodeOptions{
		Version:  nodeData.Version.Index,
		NodeSpec: nodeData.Spec,
	})
	if err != nil {
		return errors.Wrap(err, "")
	}
	return updateDBSwarmNodes(newClient)
}

func (p *swarmProvisioner) RemoveNode(opts provision.RemoveNodeOptions) error {
	node, err := p.GetNode(opts.Address)
	if err != nil {
		return err
	}
	client, err := chooseDBSwarmNode()
	if err != nil {
		return err
	}
	swarmNode := node.(*swarmNodeWrapper).Node
	if opts.Rebalance {
		swarmNode.Spec.Availability = swarm.NodeAvailabilityDrain
		err = client.UpdateNode(swarmNode.ID, docker.UpdateNodeOptions{
			NodeSpec: swarmNode.Spec,
			Version:  swarmNode.Version.Index,
		})
		if err != nil {
			return errors.Wrap(err, "")
		}
	}
	err = client.RemoveNode(docker.RemoveNodeOptions{
		ID:    swarmNode.ID,
		Force: true,
	})
	if err != nil {
		return errors.Wrap(err, "")
	}
	return updateDBSwarmNodes(client)
}

func (p *swarmProvisioner) UpdateNode(provision.UpdateNodeOptions) error {
	return errNotImplemented
}

func (p *swarmProvisioner) ArchiveDeploy(app provision.App, archiveURL string, evt *event.Event) (imgID string, err error) {
	baseImage := image.GetBuildImage(app)
	buildingImage, err := image.AppNewImageName(app.GetName())
	if err != nil {
		return "", errors.Wrap(err, "")
	}
	cmds := dockercommon.ArchiveDeployCmds(app, archiveURL)
	client, err := chooseDBSwarmNode()
	if err != nil {
		return "", err
	}
	srvID, task, err := runOnceBuildCmds(client, app, cmds, baseImage, buildingImage, evt)
	if srvID != "" {
		defer removeServiceAndLog(client, srvID)
	}
	if err != nil {
		return "", err
	}
	_, err = commitPushBuildImage(client, buildingImage, task.Status.ContainerStatus.ContainerID, app)
	if err != nil {
		return "", err
	}
	err = deployProcesses(client, app, buildingImage)
	if err != nil {
		return "", errors.Wrap(err, "")
	}
	return buildingImage, nil
}

func (p *swarmProvisioner) ImageDeploy(a provision.App, imgID string, evt *event.Event) (string, error) {
	client, err := chooseDBSwarmNode()
	if err != nil {
		return "", err
	}
	if !strings.Contains(imgID, ":") {
		imgID = fmt.Sprintf("%s:latest", imgID)
	}
	newImage, err := image.AppNewImageName(a.GetName())
	if err != nil {
		return "", errors.Wrap(err, "")
	}
	fmt.Fprintln(evt, "---- Pulling image to tsuru ----")
	var buf bytes.Buffer
	cmds := []string{"/bin/bash", "-c", "cat /home/application/current/Procfile || cat /app/user/Procfile || cat /Procfile"}
	srvID, task, err := runOnceBuildCmds(client, a, cmds, imgID, newImage, &buf)
	if srvID != "" {
		defer removeServiceAndLog(client, srvID)
	}
	if err != nil {
		return "", err
	}
	client, err = clientForNode(client, task.NodeID)
	if err != nil {
		return "", err
	}
	procfileData := buf.String()
	procfile := image.GetProcessesFromProcfile(procfileData)
	imageInspect, err := client.InspectImage(imgID)
	if err != nil {
		return "", errors.Wrap(err, "")
	}
	if len(procfile) == 0 {
		fmt.Fprintln(evt, "  ---> Procfile not found, trying to get entrypoint")
		if len(imageInspect.Config.Entrypoint) == 0 {
			return "", errors.New("no procfile or entrypoint found in image")
		}
		webProcess := imageInspect.Config.Entrypoint[0]
		for _, c := range imageInspect.Config.Entrypoint[1:] {
			webProcess += fmt.Sprintf(" %q", c)
		}
		procfile["web"] = webProcess
	}
	for k, v := range procfile {
		fmt.Fprintf(evt, "  ---> Process %s found with command: %v\n", k, v)
	}
	imageInfo := strings.Split(newImage, ":")
	repo, tag := strings.Join(imageInfo[:len(imageInfo)-1], ":"), imageInfo[len(imageInfo)-1]
	err = client.TagImage(imgID, docker.TagImageOptions{Repo: repo, Tag: tag, Force: true})
	if err != nil {
		return "", errors.Wrap(err, "")
	}
	err = pushImage(client, repo, tag)
	if err != nil {
		return "", err
	}
	imageData := image.CreateImageMetadata(newImage, procfile)
	if len(imageInspect.Config.ExposedPorts) > 1 {
		return "", errors.New("Too many ports. You should especify which one you want to.")
	}
	for k := range imageInspect.Config.ExposedPorts {
		imageData.CustomData["exposedPort"] = string(k)
	}
	err = image.SaveImageCustomData(newImage, imageData.CustomData)
	if err != nil {
		return "", errors.Wrap(err, "")
	}
	a.SetUpdatePlatform(true)
	err = deployProcesses(client, a, newImage)
	if err != nil {
		return "", err
	}
	return newImage, nil
}

func deployProcesses(client *docker.Client, a provision.App, imgID string) error {
	imageData, err := image.GetImageCustomData(imgID)
	if err != nil {
		return err
	}
	for processName, cmd := range imageData.Processes {
		err = deploy(client, a, processName, cmd, imgID)
		if err != nil {
			// TODO(cezarsa): better error handling
			return err
		}
	}
	err = image.AppendAppImageName(a.GetName(), imgID)
	if err != nil {
		return errors.Wrap(err, "")
	}
	return nil
}

func deploy(client *docker.Client, a provision.App, process, cmd, imgID string) error {
	srvName := serviceNameForApp(a, process)
	srv, err := client.InspectService(srvName)
	if err != nil {
		if _, isNotFound := err.(*docker.NoSuchService); !isNotFound {
			return errors.Wrap(err, "")
		}
	}
	var spec *swarm.ServiceSpec
	if srv == nil {
		spec, err = serviceSpecForApp(tsuruServiceOpts{
			app:     a,
			process: process,
			image:   imgID,
		})
		if err != nil {
			return err
		}
		srv, err = client.CreateService(docker.CreateServiceOptions{
			ServiceSpec: *spec,
		})
		if err != nil {
			return errors.Wrap(err, "")
		}
	} else {
		spec, err = serviceSpecForApp(tsuruServiceOpts{
			app:      a,
			process:  process,
			image:    imgID,
			baseSpec: &srv.Spec,
		})
		if err != nil {
			return err
		}
		srv.Spec = *spec
		err = client.UpdateService(srv.ID, docker.UpdateServiceOptions{
			Version:     srv.Version.Index,
			ServiceSpec: srv.Spec,
		})
		if err != nil {
			return errors.Wrap(err, "")
		}
	}
	return nil
}

func runOnceBuildCmds(client *docker.Client, a provision.App, cmds []string, imgID, buildingImage string, w io.Writer) (string, *swarm.Task, error) {
	spec, err := serviceSpecForApp(tsuruServiceOpts{
		app:        a,
		image:      imgID,
		isDeploy:   true,
		buildImage: buildingImage,
	})
	if err != nil {
		return "", nil, err
	}
	spec.TaskTemplate.ContainerSpec.Command = cmds
	spec.TaskTemplate.RestartPolicy.Condition = swarm.RestartPolicyConditionNone
	srv, err := client.CreateService(docker.CreateServiceOptions{
		ServiceSpec: *spec,
	})
	if err != nil {
		return "", nil, errors.Wrap(err, "")
	}
	createdID := srv.ID
	tasks, err := waitForTasks(client, createdID, swarm.TaskStateShutdown)
	if err != nil {
		return createdID, nil, err
	}
	client, err = clientForNode(client, tasks[0].NodeID)
	if err != nil {
		return createdID, nil, err
	}
	contID := tasks[0].Status.ContainerStatus.ContainerID
	attachOpts := docker.AttachToContainerOptions{
		Container:    contID,
		OutputStream: w,
		ErrorStream:  w,
		Logs:         true,
		Stdout:       true,
		Stderr:       true,
		Stream:       true,
	}
	exitCode, err := safeAttachWaitContainer(client, attachOpts)
	if err != nil {
		return createdID, nil, err
	}
	if exitCode != 0 {
		return createdID, nil, errors.Errorf("unexpected result code for build container: %d", exitCode)
	}
	return createdID, &tasks[0], nil
}
