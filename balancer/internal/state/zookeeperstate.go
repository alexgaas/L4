package state

import (
	"balancer/internal/log"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/go-zookeeper/zk"
)

var Log log.Logger

type ZKState struct {
	Conn *zk.Conn
}

func NewSeed(seed *[]byte) {
	rand.Seed(time.Now().UnixNano())
	*seed = make([]byte, 8)
	rand.Read(*seed)
}

func NewZKState(servers []string) (*ZKState, error) {
	zk.DefaultLogger = &ZKLogger{}

	conn, _, err := zk.Connect(servers, time.Second*10)
	if err != nil {
		return nil, err
	}

	if exists, _, err := conn.Exists("/l4"); err != nil {
		return nil, err
	} else if !exists {
		if _, err := conn.Create("/l4", nil, 0, zk.WorldACL(zk.PermAll)); err != nil {
			return nil, err
		}
	}

	if exists, _, err := conn.Exists("/l4/balancer"); err != nil {
		return nil, err
	} else if !exists {
		if _, err := conn.Create("/l4/balancer", nil, 0, zk.WorldACL(zk.PermAll)); err != nil {
			return nil, err
		}
	}

	if exists, _, err := conn.Exists("/l4/balancer/leader"); err != nil {
		return nil, err
	} else if !exists {
		if _, err := conn.Create("/l4/balancer/leader", nil, 0, zk.WorldACL(zk.PermAll)); err != nil {
			return nil, err
		}
	}

	if exists, _, err := conn.Exists("/l4/balancer/seed"); err != nil {
		return nil, err
	} else if !exists {
		if _, err := conn.Create("/l4/balancer/seed", nil, 0, zk.WorldACL(zk.PermAll)); err != nil {
			return nil, err
		}
	}

	return &ZKState{
		Conn: conn,
	}, nil
}

func (t *ZKState) Wait() error {
	// A sequential flag is used to create index.
	// Ephemerial flag is used to prevent locking clients.
	// Client with the smallest index is current leader.
	currentNode, err := t.Conn.Create("/l4/balancer/leader/client_", nil, zk.FlagEphemeral|zk.FlagSequence, zk.WorldACL(zk.PermAll))
	if err != nil {
		return err
	}

	// TODO: replace 36/7 with constants
	currentIdx, err := strconv.ParseInt(currentNode[36:], 10, 64)
	if err != nil {
		return fmt.Errorf("could not parse znode idx %v", err)
	}

	for {
		clients, _, ev, err := t.Conn.ChildrenW("/l4/balancer/leader")
		if err != nil {
			return err
		}

		minIdx := currentIdx
		for i := range clients {
			idx, err := strconv.ParseInt(clients[i][7:], 10, 64)
			if err != nil {
				return fmt.Errorf("could not parse child znode idx %v", err)
			}

			if idx < minIdx {
				minIdx = idx
			}
		}

		if currentIdx == minIdx {
			return nil
		}

		Log.Infof("Waiting to become a leader")
		// ChildrenW returns events channel which could be used
		// for process locking. After event occurs we need to call
		// ChildrenW again.
		<-ev
	}
}

func (t *ZKState) Close() {
	t.Conn.Close()
}

func (t *ZKState) GetSeed(uuid string, seed *[]byte) error {
	var err error
	var exists bool
	path := fmt.Sprintf("/l4/balancer/seed/%s", uuid)

	if exists, _, _ = t.Conn.Exists(path); exists {
		*seed, _, err = t.Conn.Get(path)
	}

	if !exists || err != nil {
		NewSeed(seed)

		_, err = t.Conn.Create(path, *seed, 0, zk.WorldACL(zk.PermAll))
	}

	if err != nil {
		return err
	}

	return nil
}

type ZKLogger struct {
}

func (t *ZKLogger) Printf(fmt string, args ...interface{}) {
	Log.Infof(fmt, args...)
}
