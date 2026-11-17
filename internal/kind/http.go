package kind

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/worldtree-industries/ivaldi/internal/manifest"
	"github.com/worldtree-industries/ivaldi/internal/scaffold"
)

// firstPort is where the default port search starts.
const firstPort = 8080

const maxPort = 65535

// HTTP returns the http Kind: a Deployable HTTP server built on mjolnir's
// router, listening on a port unique within the Project.
func HTTP() Kind {
	return Kind{
		Name:    "http",
		Ask:     askHTTP,
		Modules: []string{mjolnirModule},
	}
}

func askHTTP(q *Questions, name string) (Answers, error) {
	used := usedPorts(q.Binaries)
	port := firstPort
	for used[port] {
		port++
	}
	answer, err := q.Prompt.Validated("Port for "+name, strconv.Itoa(port), func(s string) error {
		return validatePort(s, used)
	})
	if err != nil {
		return Answers{}, err
	}
	port, _ = strconv.Atoi(answer)
	answers := Answers{Settings: []manifest.Setting{{Key: "port", Value: port}}}

	clientDoc, err := scaffold.LayerDoc(scaffold.LayerClient)
	if err != nil {
		return Answers{}, err
	}
	exists, err := clientExists(q, clientDoc.Path)
	if err != nil {
		return Answers{}, err
	}
	if !exists && q.Prompt.Bool("Create a client layer (other Projects may import it)?", false) {
		answers.Files = append(answers.Files, clientDoc)
	}
	return answers, nil
}

// usedPorts are the ports of every binary that has one.
func usedPorts(binaries []manifest.Binary) map[int]bool {
	used := map[int]bool{}
	for _, b := range binaries {
		if port, err := strconv.Atoi(b.Settings["port"]); err == nil {
			used[port] = true
		}
	}
	return used
}

var errInvalidPort = errors.New("not a port")

func validatePort(s string, used map[int]bool) error {
	port, err := strconv.Atoi(s)
	if err != nil || port < 1 || port > maxPort {
		return fmt.Errorf("%w: use a number from 1 to %d", errInvalidPort, maxPort)
	}
	if used[port] {
		return fmt.Errorf("port %d is already used in %s", port, manifest.FileName)
	}
	return nil
}

// clientExists reports whether the client Layer is on disk or planned by an
// earlier binary in this run.
func clientExists(q *Questions, doc string) (bool, error) {
	for _, rel := range []string{scaffold.LayerClient, doc} {
		if exists, err := q.Exists(rel); err != nil || exists {
			return exists, err
		}
	}
	return false, nil
}
