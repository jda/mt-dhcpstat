// Fetch DHCP pool stats from a Mikrotik router
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Netwurx/routeros-api-go"
	"os"
	"strconv"
	"strings"
)

type jsError struct {
	Error string
}

type PoolStat struct {
	Interface string
	Used      int
	Size      int
}

func jserror(msg string) {
	e := jsError{msg}
	m, err := json.Marshal(e)
	if err != nil {
		panic(err)
	}
	os.Stderr.Write(m)
	os.Exit(1)
}

type IPRange struct {
	Min string
	Max string
}

// Get addresses assigned to pool
func getPoolUsed(r *routeros.Client, pool string) ([]string, error) {
	var addrs []string

	var q routeros.Query
	q.Pairs = append(q.Pairs, routeros.Pair{Key: "pool", Value: pool, Op: "="})
	q.Proplist = []string{".id,address"}
	res, err := r.Query("/ip/pool/used/print", q)
	if err != nil {
		return addrs, err
	}

	for _, s := range res.SubPairs {
		addrs = append(addrs, s["address"])
	}

	return addrs, nil
}

// Get settings for named IP Pool
func getPoolRange(r *routeros.Client, pool string) ([]IPRange, error) {
	var ranges []IPRange

	var q routeros.Query
	q.Pairs = append(q.Pairs, routeros.Pair{Key: "name", Value: pool, Op: "="})
	q.Proplist = []string{".id,ranges"}
	res, err := r.Query("/ip/pool/print", q)
	if err != nil {
		return ranges, err
	}

	// no results so bail
	if len(res.SubPairs) == 0 {
		return ranges, nil
	}

	sp := res.SubPairs[0]

	// no ranges defined so bail
	if sp["ranges"] == "" {
		return ranges, nil
	}

	for _, p := range strings.Split(sp["ranges"], ",") {
		var ipr IPRange
		rng := strings.Split(p, "-")
		ipr.Min = rng[0]
		ipr.Max = rng[1]
		ranges = append(ranges, ipr)
	}

	return ranges, nil
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [options] router-address\n", os.Args[0])
	fmt.Fprintln(os.Stderr, "Login credentials must be set as environment variables")
	fmt.Fprintln(os.Stderr, "Username in MT_USERNAME")
	fmt.Fprintln(os.Stderr, "Password in MT_PASSWORD")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	flag.Usage = usage
	js := false
	flag.BoolVar(&js, "json", false, "display output in json format")
	port := 8729
	flag.IntVar(&port, "port", 8728, "RouterOS API port number")
	flag.Parse()

	// check for host
	args := flag.Args()
	if len(args) != 1 {
		usage()
	}
	host := args[0]

	// check for login credentials
	username := os.Getenv("MT_USERNAME")
	if username == "" {
		if js {
			jserror("MT_USERNAME is empty or not set")
		}
		fmt.Fprintln(os.Stderr, "Error: MT_USERNAME is empty or not set")
		usage()
	}

	// don't check for password because missing password is valid (just not good)
	password := os.Getenv("MT_PASSWORD")

	// try to connect to router
	hp := host + ":" + strconv.Itoa(port)
	r, err := routeros.New(hp)
	if err != nil {
		if js {
			jserror("Invalid address for router")
		}
		fmt.Fprintf(os.Stderr, "Invalid address for router: %s\n", err)
		os.Exit(1)
	}
	err = r.Connect(username, password)
	if err != nil {
		if js {
			jserror("Error connecting to router")
		}
		fmt.Fprintf(os.Stderr, "Error connecting to router: %s\n", err)
		os.Exit(1)
	}

	// get all dhcp servers
	var dsQ routeros.Query
	dsQ.Pairs = append(dsQ.Pairs, routeros.Pair{Key: "disabled", Value: "no", Op: "="})
	dsQ.Proplist = []string{".id,address-pool,interface"}
	res, err := r.Query("/ip/dhcp-server/getall", dsQ)
	if err != nil {
		if js {
			jserror("Error fetching list of dhcp interfaces from router")
		}
		fmt.Fprintf(os.Stderr, "Error fetching list of dhcp interfaces: %s\n", err)
		os.Exit(1)
	}

	if !js {
		fmt.Println("Interface\tUsed\tFree")
	}
	var pools []PoolStat
	for _, pair := range res.SubPairs {
		var p PoolStat
		p.Interface = pair["interface"]

		// get pool range, find matching subnet
		poolName := pair["address-pool"]
		if poolName == "" {
			continue // no pool so we can't do any sizing
		}

		pool, err := getPoolRange(r, poolName)
		if err != nil {
			if js {
				jserror("Error fetching pool information for pool " + poolName)
			}
			fmt.Fprintf(os.Stderr, "Error fetching pool information: %s\n", err)
			os.Exit(1)
		}

		// calculate size of pool. we don't need cidr math because crossing .0 and .255 leads to
		// user issues so nobody uses those addrs
		size := 0
		for _, l := range pool {
			minparts := strings.Split(l.Min, ".")
			min, _ := strconv.Atoi(minparts[3])
			maxparts := strings.Split(l.Max, ".")
			max, _ := strconv.Atoi(maxparts[3])

			size += max - min
		}
		p.Size = size

		used, err := getPoolUsed(r, poolName)
		if err != nil {
			if js {
				jserror("Error fetching pool usage for pool " + poolName)
			}
			fmt.Fprintf(os.Stderr, "Error fetching pool usage for pool %s: %s\n", poolName, err)
			os.Exit(1)
		}
		p.Used = len(used)

		if !js {
			fmt.Printf("%s\t%12d\t%4d\n", p.Interface, p.Used, (p.Size - p.Used))
		}

		pools = append(pools, p)
	}

	if js {
		j, err := json.Marshal(pools)
		if err != nil {
			jserror("Error encoding json representation of pools")
		}
		fmt.Printf("%s", j)
	}
	/*
		what i was going to do before stopping for sleep:
		loop through pools, check usage, return info, append to PoolStats
		after that we format stuff for output to user
	*/
}
