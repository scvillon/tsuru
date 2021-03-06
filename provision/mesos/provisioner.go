// Copyright 2016 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mesos

import (
	"errors"
	"io"

	"github.com/tsuru/tsuru/event"
	"github.com/tsuru/tsuru/provision"
)

const (
	provisionerName = "mesos"
)

var errNotImplemented = errors.New("not implemented")

type mesosProvisioner struct{}

func init() {
	provision.Register(provisionerName, func() (provision.Provisioner, error) {
		return &mesosProvisioner{}, nil
	})
}

func (p *mesosProvisioner) GetName() string {
	return provisionerName
}

func (p *mesosProvisioner) Provision(provision.App) error {
	return errNotImplemented
}

func (p *mesosProvisioner) Destroy(provision.App) error {
	return errNotImplemented
}

func (p *mesosProvisioner) AddUnits(provision.App, uint, string, io.Writer) ([]provision.Unit, error) {
	return nil, errNotImplemented
}

func (p *mesosProvisioner) RemoveUnits(provision.App, uint, string, io.Writer) error {
	return errNotImplemented
}

func (p *mesosProvisioner) SetUnitStatus(provision.Unit, provision.Status) error {
	return errNotImplemented
}

func (p *mesosProvisioner) Restart(provision.App, string, io.Writer) error {
	return errNotImplemented
}

func (p *mesosProvisioner) Start(provision.App, string) error {
	return errNotImplemented
}

func (p *mesosProvisioner) Stop(provision.App, string) error {
	return errNotImplemented
}

func (p *mesosProvisioner) Units(app provision.App) ([]provision.Unit, error) {
	return nil, errNotImplemented
}

func (p *mesosProvisioner) RoutableUnits(app provision.App) ([]provision.Unit, error) {
	return nil, errNotImplemented
}

func (p *mesosProvisioner) RegisterUnit(unit provision.Unit, customData map[string]interface{}) error {
	return errNotImplemented
}

func (p *mesosProvisioner) ListNodes(addressFilter []string) ([]provision.Node, error) {
	return nil, errNotImplemented
}

func (p *mesosProvisioner) GetNode(address string) (provision.Node, error) {
	return nil, errNotImplemented
}

func (p *mesosProvisioner) AddNode(opts provision.AddNodeOptions) error {
	return errNotImplemented
}

func (p *mesosProvisioner) RemoveNode(opts provision.RemoveNodeOptions) error {
	return errNotImplemented
}

func (p *mesosProvisioner) UpdateNode(provision.UpdateNodeOptions) error {
	return errNotImplemented
}

func (p *mesosProvisioner) ArchiveDeploy(app provision.App, archiveURL string, evt *event.Event) (imgID string, err error) {
	return "", errNotImplemented
}

func (p *mesosProvisioner) ImageDeploy(a provision.App, imgID string, evt *event.Event) (string, error) {
	return "", errNotImplemented
}
