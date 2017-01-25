package storeapi

import (
	"io/ioutil"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarmkit/api"
	cautils "github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
)

type mockProposer struct {
	index uint64
}

func (mp *mockProposer) ProposeValue(ctx context.Context, storeAction []api.StoreAction, cb func()) error {
	if cb != nil {
		cb()
	}
	return nil
}

func (mp *mockProposer) GetVersion() *api.Version {
	mp.index += 3
	return &api.Version{Index: mp.index}
}

type testServer struct {
	Server *Server
	Client api.StoreClient
	Store  *store.MemoryStore

	grpcServer *grpc.Server
	clientConn *grpc.ClientConn

	tempUnixSocket string
}

func (ts *testServer) Stop() {
	ts.clientConn.Close()
	ts.grpcServer.Stop()
	ts.Store.Close()
	os.RemoveAll(ts.tempUnixSocket)
}

func newTestServer(t *testing.T) *testServer {
	ts := &testServer{}

	// Create a testCA just to get a usable RootCA object
	tc := cautils.NewTestCA(nil)
	tc.Stop()

	ts.Store = store.NewMemoryStore(&mockProposer{})
	assert.NotNil(t, ts.Store)
	ts.Server = NewServer(ts.Store)
	assert.NotNil(t, ts.Server)

	temp, err := ioutil.TempFile("", "test-socket")
	assert.NoError(t, err)
	assert.NoError(t, temp.Close())
	assert.NoError(t, os.Remove(temp.Name()))

	ts.tempUnixSocket = temp.Name()

	lis, err := net.Listen("unix", temp.Name())
	assert.NoError(t, err)

	ts.grpcServer = grpc.NewServer()
	api.RegisterStoreServer(ts.grpcServer, ts.Server)
	go func() {
		// Serve will always return an error (even when properly stopped).
		// Explicitly ignore it.
		_ = ts.grpcServer.Serve(lis)
	}()

	conn, err := grpc.Dial(temp.Name(), grpc.WithInsecure(), grpc.WithTimeout(10*time.Second),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}))
	assert.NoError(t, err)
	ts.clientConn = conn

	ts.Client = api.NewStoreClient(conn)

	return ts
}

func createNode(t *testing.T, ts *testServer, id string, role api.NodeRole, membership api.NodeSpec_Membership, state api.NodeStatus_State) *api.Node {
	node := &api.Node{
		ID: id,
		Spec: api.NodeSpec{
			Membership: membership,
		},
		Status: api.NodeStatus{
			State: state,
		},
		Role: role,
	}
	err := ts.Store.Update(func(tx store.Tx) error {
		return store.CreateNode(tx, node)
	})
	assert.NoError(t, err)
	return node
}

func init() {
	grpclog.SetLogger(log.New(ioutil.Discard, "", log.LstdFlags))
	logrus.SetOutput(ioutil.Discard)
}
