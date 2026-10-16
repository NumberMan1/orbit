//go:build integration

package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Launches an isolated local etcd; never connects to an existing cluster.
func testEtcd(t *testing.T) *clientv3.Client {
	t.Helper()
	binary, err := exec.LookPath("etcd")
	if err != nil {
		t.Skip("etcd binary is required for integration tests")
	}
	reserve := func() net.Listener {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		return ln
	}
	c, p := reserve(), reserve()
	clientURL, peerURL := "http://"+c.Addr().String(), "http://"+p.Addr().String()
	c.Close()
	p.Close()
	dir := t.TempDir()
	log, err := os.Create(filepath.Join(dir, "etcd.log"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, "--name", "orbit-test", "--data-dir", filepath.Join(dir, "data"), "--listen-client-urls", clientURL, "--advertise-client-urls", clientURL, "--listen-peer-urls", peerURL, "--initial-advertise-peer-urls", peerURL, "--initial-cluster", "orbit-test="+peerURL, "--log-level", "error")
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		cancel()
		log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait(); log.Close() })
	client, err := clientv3.New(clientv3.Config{Endpoints: []string{clientURL}, DialTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	deadline := time.Now().Add(10 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err = client.Status(ctx, clientURL)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(filepath.Join(dir, "etcd.log"))
			t.Fatalf("etcd startup: %v\n%s", err, data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return client
}

func TestEtcdRegistrationWatchAndLeaseLoss(t *testing.T) {
	client := testEtcd(t)
	r, err := New(client, "/orbit/integration")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	d := Descriptor{ID: "one", Name: "worker", Address: "127.0.0.1:1"}
	registration, err := r.Register(ctx, d, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close(context.Background())
	if duplicate, err := r.Register(ctx, d, 3); !errors.Is(err, ErrIDInUse) {
		if duplicate != nil {
			duplicate.Close(context.Background())
		}
		t.Fatalf("duplicate = %v", err)
	}
	// KeepAlive responses must be consumed continuously beyond the original TTL.
	time.Sleep(4 * time.Second)
	response, err := client.Get(ctx, r.prefix+d.ID)
	if err != nil || len(response.Kvs) != 1 {
		t.Fatalf("registration expired: %v", err)
	}
	lease := clientv3.LeaseID(response.Kvs[0].Lease)
	var second *Registration
	calls := 0
	finished := errors.New("observed gap-free snapshot")
	err = r.Watch(ctx, func(snapshot map[string]Descriptor) error {
		calls++
		if calls == 1 {
			if len(snapshot) != 1 {
				return fmt.Errorf("initial snapshot: %v", snapshot)
			}
			// This registration occurs strictly between Get and Watch setup.
			var err error
			second, err = r.Register(ctx, Descriptor{ID: "two", Name: "worker", Address: "127.0.0.1:2"}, 3)
			return err
		}
		if len(snapshot) == 2 {
			return finished
		}
		return nil
	})
	if second != nil {
		defer second.Close(context.Background())
	}
	if !errors.Is(err, finished) {
		t.Fatalf("watch: %v", err)
	}
	if _, err := client.Revoke(ctx, lease); err != nil {
		t.Fatal(err)
	}
	select {
	case <-registration.Done():
		if !errors.Is(registration.Err(), ErrLeaseLost) {
			t.Fatal(registration.Err())
		}
	case <-ctx.Done():
		t.Fatal("lease loss not reported")
	}
	if err := second.Close(ctx); err != nil {
		t.Fatal(err)
	}
	response, err = client.Get(ctx, r.prefix, clientv3.WithPrefix())
	if err != nil || len(response.Kvs) != 0 {
		t.Fatalf("registrations not revoked: %v", err)
	}
}

func TestRegistrarRecoversDeletionWithoutOverwritingSuccessor(t *testing.T) {
	client := testEtcd(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	r, _ := New(client, "/registrar")
	d := Descriptor{ID: "one", Name: "worker", Address: "localhost:1"}
	registrar, _ := r.NewRegistrar(d, RegistrarOptions{TTL: 3, RetryMin: 10 * time.Millisecond, RetryMax: 30 * time.Millisecond})
	if err := registrar.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer registrar.Stop(ctx)
	get := func() *clientv3.GetResponse {
		t.Helper()
		response, err := client.Get(ctx, r.prefix+d.ID)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	wait := func(predicate func() bool) {
		t.Helper()
		for !predicate() {
			select {
			case <-ctx.Done():
				t.Fatal("recovery timeout", registrar.Status())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	first := get().Kvs[0].Lease
	if _, err := client.Delete(ctx, r.prefix+d.ID); err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		res := get()
		return len(res.Kvs) == 1 && res.Kvs[0].Lease != first && registrar.Status().Ready
	})
	// Install an unrelated owner atomically. The registrar must observe loss,
	// then refuse to reclaim that ID until the other owner releases it.
	successor, err := client.Grant(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Put(ctx, r.prefix+d.ID, `{"id":"one","name":"other","address":"localhost:2"}`, clientv3.WithLease(successor.ID)); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return errors.Is(registrar.Status().LastError, ErrIDInUse) })
	if registrar.Status().Ready || get().Kvs[0].Lease != int64(successor.ID) {
		t.Fatal("successor overwritten")
	}
	client.Revoke(ctx, successor.ID)
	wait(func() bool { res := get(); return len(res.Kvs) == 1 && registrar.Status().Ready })
	if err := registrar.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if len(get().Kvs) != 0 {
		t.Fatal("lease retained after Stop")
	}
}

func TestRegistrationUpdateChecksOwnership(t *testing.T) {
	client := testEtcd(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	r, _ := New(client, "/updates")
	d := Descriptor{ID: "one", Name: "worker", Address: "localhost:1"}
	registration, err := r.Register(ctx, d, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close(ctx)
	d.Metadata = map[string]string{"state": "ready"}
	if err := registration.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(ctx, r.prefix+d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if response.Kvs[0].Lease != int64(registration.lease) {
		t.Fatal("update removed lease")
	}
	if _, err := client.Put(ctx, r.prefix+d.ID, "successor"); err != nil {
		t.Fatal(err)
	}
	if err := registration.Update(ctx, d); !errors.Is(err, ErrLeaseLost) {
		t.Fatal(err)
	}
	<-registration.Done()
	response, err = client.Get(ctx, r.prefix+d.ID)
	if err != nil || len(response.Kvs) != 1 || string(response.Kvs[0].Value) != "successor" {
		t.Fatal(response, err)
	}
}
