package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"

	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/lxd/cluster"
	"github.com/lxc/lxd/lxd/db"
	"github.com/lxc/lxd/lxd/util"
	"github.com/lxc/lxd/shared"
	"github.com/lxc/lxd/shared/api"
	"github.com/lxc/lxd/shared/version"
)

var profilesCmd = Command{
	name: "profiles",
	get:  profilesGet,
	post: profilesPost,
}

var profileCmd = Command{
	name:   "profiles/{name}",
	get:    profileGet,
	put:    profilePut,
	delete: profileDelete,
	post:   profilePost,
	patch:  profilePatch,
}

/* This is used for both profiles post and profile put */
func profilesGet(d *Daemon, r *http.Request) Response {
	project := projectParam(r)

	recursion := util.IsRecursionRequest(r)

	var result interface{}
	err := d.cluster.Transaction(func(tx *db.ClusterTx) error {
		hasProfiles, err := tx.ProjectHasProfiles(project)
		if err != nil {
			return errors.Wrap(err, "Check project features")
		}

		if !hasProfiles {
			project = "default"
		}

		filter := db.ProfileFilter{
			Project: project,
		}
		if recursion {
			profiles, err := tx.ProfileList(filter)
			if err != nil {
				return err
			}
			apiProfiles := make([]*api.Profile, len(profiles))
			for i, profile := range profiles {
				apiProfiles[i] = db.ProfileToAPI(&profile)
			}

			result = apiProfiles
		} else {
			result, err = tx.ProfileURIs(filter)
		}
		return err
	})
	if err != nil {
		return SmartError(err)
	}

	return SyncResponse(true, result)
}

func profilesPost(d *Daemon, r *http.Request) Response {
	project := projectParam(r)
	req := api.ProfilesPost{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return BadRequest(err)
	}

	// Sanity checks
	if req.Name == "" {
		return BadRequest(fmt.Errorf("No name provided"))
	}

	_, profile, _ := d.cluster.ProfileGet(project, req.Name)
	if profile != nil {
		return BadRequest(fmt.Errorf("The profile already exists"))
	}

	if strings.Contains(req.Name, "/") {
		return BadRequest(fmt.Errorf("Profile names may not contain slashes"))
	}

	if shared.StringInSlice(req.Name, []string{".", ".."}) {
		return BadRequest(fmt.Errorf("Invalid profile name '%s'", req.Name))
	}

	err := containerValidConfig(d.os, req.Config, true, false)
	if err != nil {
		return BadRequest(err)
	}

	err = containerValidDevices(d.cluster, req.Devices, true, false)
	if err != nil {
		return BadRequest(err)
	}

	// Update DB entry
	err = d.cluster.Transaction(func(tx *db.ClusterTx) error {
		profile := db.Profile{
			Project:     project,
			Name:        req.Name,
			Description: req.Description,
			Config:      req.Config,
			Devices:     req.Devices,
		}
		_, err := tx.ProfileCreate(profile)
		return err
	})
	if err != nil {
		return SmartError(
			fmt.Errorf("Error inserting %s into database: %s", req.Name, err))
	}

	return SyncResponseLocation(true, nil, fmt.Sprintf("/%s/profiles/%s", version.APIVersion, req.Name))
}

func profileGet(d *Daemon, r *http.Request) Response {
	project := projectParam(r)
	name := mux.Vars(r)["name"]

	var resp *api.Profile

	err := d.cluster.Transaction(func(tx *db.ClusterTx) error {
		hasProfiles, err := tx.ProjectHasProfiles(project)
		if err != nil {
			return errors.Wrap(err, "Check project features")
		}

		if !hasProfiles {
			project = "default"
		}

		profile, err := tx.ProfileGet(project, name)
		if err != nil {
			return errors.Wrap(err, "Fetch profile")
		}

		resp = db.ProfileToAPI(profile)

		return nil
	})
	if err != nil {
		return SmartError(err)
	}

	etag := []interface{}{resp.Config, resp.Description, resp.Devices}
	return SyncResponseETag(true, resp, etag)
}

func profilePut(d *Daemon, r *http.Request) Response {
	// Get the project
	project := projectParam(r)

	// Get the profile
	name := mux.Vars(r)["name"]

	if isClusterNotification(r) {
		// In this case the ProfilePut request payload contains
		// information about the old profile, since the new one has
		// already been saved in the database.
		old := api.ProfilePut{}
		err := json.NewDecoder(r.Body).Decode(&old)
		if err != nil {
			return BadRequest(err)
		}
		err = doProfileUpdateCluster(d, project, name, old)
		return SmartError(err)

	}

	id, profile, err := d.cluster.ProfileGet(project, name)
	if err != nil {
		return SmartError(fmt.Errorf("Failed to retrieve profile='%s'", name))
	}

	// Validate the ETag
	etag := []interface{}{profile.Config, profile.Description, profile.Devices}
	err = util.EtagCheck(r, etag)
	if err != nil {
		return PreconditionFailed(err)
	}

	req := api.ProfilePut{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return BadRequest(err)
	}

	err = doProfileUpdate(d, project, name, id, profile, req)

	if err == nil && !isClusterNotification(r) {
		// Notify all other nodes. If a node is down, it will be ignored.
		notifier, err := cluster.NewNotifier(d.State(), d.endpoints.NetworkCert(), cluster.NotifyAlive)
		if err != nil {
			return SmartError(err)
		}
		err = notifier(func(client lxd.ContainerServer) error {
			return client.UpdateProfile(name, profile.ProfilePut, "")
		})
		if err != nil {
			return SmartError(err)
		}
	}

	return SmartError(err)
}

func profilePatch(d *Daemon, r *http.Request) Response {
	// Get the project
	project := projectParam(r)

	// Get the profile
	name := mux.Vars(r)["name"]
	id, profile, err := d.cluster.ProfileGet(project, name)
	if err != nil {
		return SmartError(fmt.Errorf("Failed to retrieve profile='%s'", name))
	}

	// Validate the ETag
	etag := []interface{}{profile.Config, profile.Description, profile.Devices}
	err = util.EtagCheck(r, etag)
	if err != nil {
		return PreconditionFailed(err)
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return InternalError(err)
	}

	rdr1 := ioutil.NopCloser(bytes.NewBuffer(body))
	rdr2 := ioutil.NopCloser(bytes.NewBuffer(body))

	reqRaw := shared.Jmap{}
	if err := json.NewDecoder(rdr1).Decode(&reqRaw); err != nil {
		return BadRequest(err)
	}

	req := api.ProfilePut{}
	if err := json.NewDecoder(rdr2).Decode(&req); err != nil {
		return BadRequest(err)
	}

	// Get Description
	_, err = reqRaw.GetString("description")
	if err != nil {
		req.Description = profile.Description
	}

	// Get Config
	if req.Config == nil {
		req.Config = profile.Config
	} else {
		for k, v := range profile.Config {
			_, ok := req.Config[k]
			if !ok {
				req.Config[k] = v
			}
		}
	}

	// Get Devices
	if req.Devices == nil {
		req.Devices = profile.Devices
	} else {
		for k, v := range profile.Devices {
			_, ok := req.Devices[k]
			if !ok {
				req.Devices[k] = v
			}
		}
	}

	return SmartError(doProfileUpdate(d, project, name, id, profile, req))
}

// The handler for the post operation.
func profilePost(d *Daemon, r *http.Request) Response {
	project := projectParam(r)
	name := mux.Vars(r)["name"]

	if name == "default" {
		return Forbidden(errors.New("The 'default' profile cannot be renamed"))
	}

	req := api.ProfilePost{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return BadRequest(err)
	}

	// Sanity checks
	if req.Name == "" {
		return BadRequest(fmt.Errorf("No name provided"))
	}

	if strings.Contains(req.Name, "/") {
		return BadRequest(fmt.Errorf("Profile names may not contain slashes"))
	}

	if shared.StringInSlice(req.Name, []string{".", ".."}) {
		return BadRequest(fmt.Errorf("Invalid profile name '%s'", req.Name))
	}

	err := d.cluster.Transaction(func(tx *db.ClusterTx) error {
		// Check that the name isn't already in use
		_, err := tx.ProfileGet(project, req.Name)
		if err == nil {
			return fmt.Errorf("Name '%s' already in use", req.Name)
		}

		return tx.ProfileRename(project, name, req.Name)
	})
	if err != nil {
		return SmartError(err)
	}

	return SyncResponseLocation(true, nil, fmt.Sprintf("/%s/profiles/%s", version.APIVersion, req.Name))
}

// The handler for the delete operation.
func profileDelete(d *Daemon, r *http.Request) Response {
	project := projectParam(r)
	name := mux.Vars(r)["name"]

	if name == "default" {
		return Forbidden(errors.New("The 'default' profile cannot be deleted"))
	}

	_, _, err := d.cluster.ProfileGet(project, name)
	if err != nil {
		return SmartError(err)
	}

	clist := getContainersWithProfile(d.State(), project, name)
	if len(clist) != 0 {
		return BadRequest(fmt.Errorf("Profile is currently in use"))
	}

	err = d.cluster.Transaction(func(tx *db.ClusterTx) error {
		return tx.ProfileDelete(project, name)
	})
	if err != nil {
		return SmartError(err)
	}

	return EmptySyncResponse
}
