// Pani Networks proprietary license.
// (c) Copyright 2017 Pani Networks. All Rights Reserved

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/romana/core/common"
	"github.com/romana/core/common/api"
	"github.com/romana/core/routepublisher/bird"
	"github.com/romana/core/routepublisher/publisher"

	log "github.com/romana/rlog"
)

func main() {
	var err error

	hostname := flag.String("hostname", "", "name of the host in romana database")
	flagTemplateFile := flag.String("template", "/etc/bird/bird.conf.t", "template file for bird config")
	flagBirdConfigFile := flag.String("config", "/etc/bird/bird.conf", "location of the bird config file")
	flagBirdPidFile := flag.String("pid", "/var/run/bird.pid", "location of bird pid file")
	flagDebug := flag.String("debug", "", "set to yes or true to enable debug output")
	flagLocalAS := flag.String("as", "65534", "local AS number")
	flagRouterID := flag.String("id", "", "string to use as router id (hostname by default)")
	flagTopologyUrl := flag.String("url", "http://localhost:9600/topology", "url to access romanad topology description")
	flagPollPeriod := flag.Int("poll-period", 2, "sleep period between updates, in seconds")
	flagGoBgpTarget := flag.String("gobgp-target", "", "grpc endpoint of gobgp server to export prefix routes")
	flag.Parse()

	fmt.Println(common.BuildInfo())

	config := make(map[string]string)
	config["templateFileName"] = *flagTemplateFile
	config["birdConfigName"] = *flagBirdConfigFile
	config["pidFile"] = *flagBirdPidFile
	config["localAS"] = *flagLocalAS
	config["debug"] = *flagDebug

	bird, err := bird.New(publisher.Config(config))
	if err != nil {
		log.Errorf("Failed to create publisher instance, can't continue. %s", err)
		os.Exit(1)
	}

	args := make(map[string]interface{})

	if *hostname == "" {
		*hostname, err = os.Hostname()
		if err != nil {
			log.Errorf("No hostname provided and can't be determined %s", err)
			os.Exit(1)
		}
	}

	args["Hostname"] = *hostname
	args["RouterID"] = *flagRouterID
	if args["RouterID"] == "" {
		args["RouterID"] = *hostname
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := time.NewTicker(time.Duration(*flagPollPeriod) * time.Second)
	topoChan := PollTopology(ctx, *flagTopologyUrl, ticker)

	syncChan := make(chan topologyMap)
	if *flagGoBgpTarget != "" {
		bgpTicker := time.NewTicker(time.Duration(*flagPollPeriod) * time.Second)
		goBgpWorker(ctx, *flagGoBgpTarget, syncChan, bgpTicker)
	}

	for {
		select {
		case topology := <-topoChan:
			startTime := time.Now()
			args["Topology"] = topology

			bird.Update(nil, args)

			if *flagGoBgpTarget != "" {
				syncChan <- topology
			}

			runTime := time.Now().Sub(startTime)
			log.Tracef(4, "Time between route table flush and route table rebuild %s", runTime)

		}
	}
}

// topologyMap stores host IPs grouped by prefix and grouped by network.
type topologyMap map[string]map[string][]string

// PollTopology polls romanad and produces topologyMaps.
func PollTopology(ctx context.Context, url string, ticker *time.Ticker) <-chan topologyMap {
	out := make(chan topologyMap)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t, err := getTopology(url)
				if err != nil {
					log.Errorf(err.Error())
					continue
				}

				out <- t
			}
		}
	}()

	return out
}

// getTopology parses retrieves topology description from romanad
// and parses it into a topologyMap.
func getTopology(url string) (topologyMap, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var topo api.TopologyUpdateRequest
	err = json.Unmarshal(data, &topo)
	if err != nil {
		return nil, err
	}

	var topoMap topologyMap
	topoMap = make(map[string]map[string][]string)

	// walks the api.GroupOrHost tree recursively
	// and collects group-prefix=>[host1, ... hostN] relations.
	var walk func(prev, current *api.GroupOrHost, collector map[string][]string)
	walk = func(prev, current *api.GroupOrHost, collector map[string][]string) {
		if current == nil {
			return
		}

		// for every node with IP defined, make a record in collector map
		// where key is a cidr of a parent node.
		if current.IP != nil {

			// valid topology must have cidr defined for every node which has
			// child node with defined IP address.
			if prev == nil || prev.CIDR == "" {
				log.Errorf("Can not detect host group for host: %s", current.IP)
				return
			}

			collector[prev.CIDR] = append(collector[prev.CIDR], current.IP.String())
		}

		for _, next := range current.Groups {
			walk(current, &next, collector)
		}

		return
	}

	for _, t := range topo.Topologies {
		netmap := make(map[string][]string)

		if len(t.Networks) == 0 {
			return nil, fmt.Errorf("can't parse topology which has 0 items in .Networks field")
		}

		// for whatever reason we have .Networks as a list while only ever interested
		// in first value.
		topoMap[t.Networks[0]] = netmap

		for _, group := range t.Map {
			walk(nil, &group, netmap)
		}
	}

	return topoMap, nil
}
