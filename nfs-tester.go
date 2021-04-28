package main

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joshuarobinson/go-nfs-client/nfs"
	"github.com/joshuarobinson/go-nfs-client/nfs/rpc"
)

const nfsTestPeriod = 60

type NFSTester struct {
	nfshost     string
	export      string
	concurrency int

	wg                        sync.WaitGroup
	atm_finished              int32
	atm_counter_bytes_written uint64
	atm_counter_bytes_read    uint64

	filesWritten int
}

func NewNFSTester(nfshost string, export string, concurrency int) (*NFSTester, error) {

	if len(nfshost) == 0 || len(export) == 0 {
		err := errors.New("[error] Must specify host and export.")
		return nil, err
	}

	nfsTester := &NFSTester{nfshost: nfshost, export: export, concurrency: concurrency, filesWritten: 0}

	// Try and mount to verify
	mount, err := nfs.DialMount(nfshost, false)
	if err != nil {
		err := errors.New("[error] Unable to dial mount service.")
		return nil, err
	}
	defer mount.Close()

	auth := rpc.NewAuthUnix("anon", 1001, 1001)

	target, err := mount.Mount(export, auth.Auth(), false)
	if err != nil {
		err := errors.New("[error] Unable to mount export.")
		return nil, err
	}
	defer target.Close()

	return nfsTester, err
}

func (n *NFSTester) writeOneFile(fname string) {

	defer n.wg.Done()

	mount, err := nfs.DialMount(n.nfshost, false)
	if err != nil {
		fmt.Println("Portmapper failed.")
		fmt.Println(err)
		return
	}
	auth := rpc.NewAuthUnix("anon", 1001, 1001)
	target, err := mount.Mount(n.export, auth.Auth(), false)
	if err != nil {
		fmt.Println("Unable to mount.")
		fmt.Println(err)
		return
	}

	srcBuf := make([]byte, 1024*1024)
	rand.Read(srcBuf)

	f, err := target.OpenFile(fname, os.FileMode(int(0744)))
	if err != nil {
		fmt.Printf("OpenFile %s failed\n", fname)
		fmt.Println(err)
		return
	}

	var bytes_written uint64
	bytes_written = 0

	for atomic.LoadInt32(&n.atm_finished) == 0 {
		n, _ := f.Write(srcBuf)
		bytes_written += uint64(n)
	}

	atomic.AddUint64(&n.atm_counter_bytes_written, bytes_written)
}

func generateTestFilename(i int) string {

	baseDir := "/"
	fname := baseDir + "filename" + strconv.Itoa(i)
	return fname
}

func (n *NFSTester) WriteTest() float64 {

	atomic.StoreInt32(&n.atm_finished, 0)
	atomic.StoreUint64(&n.atm_counter_bytes_written, 0)

	for i := 1; i <= n.concurrency; i++ {
		fname := generateTestFilename(i)
		n.wg.Add(1)
		go n.writeOneFile(fname)
	}

	time.Sleep(nfsTestPeriod * time.Second)
	atomic.StoreInt32(&n.atm_finished, 1)
	n.wg.Wait()
	n.filesWritten += n.concurrency

	total_bytes := atomic.LoadUint64(&n.atm_counter_bytes_written)
	return float64(total_bytes) / float64(nfsTestPeriod)
}

func (n *NFSTester) readOneFile(fname string) {

	defer n.wg.Done()

	mount, err := nfs.DialMount(n.nfshost, false)
	if err != nil {
		fmt.Println(err)
		return
	}
	auth := rpc.NewAuthUnix("anon", 1001, 1001)
	target, err := mount.Mount(n.export, auth.Auth(), false)
	if err != nil {
		fmt.Println(err)
		return
	}

	p := make([]byte, 512*1024)
	byte_counter := uint64(0)
	for {
		f, err := target.Open(fname)
		if err != nil {
			fmt.Println(err)
			return
		}

		for {
			if atomic.LoadInt32(&n.atm_finished) == 1 {
				atomic.AddUint64(&n.atm_counter_bytes_read, byte_counter)
				return
			}

			n, err := f.Read(p)
			if err == io.EOF {
				break
			}
			byte_counter += uint64(n)
		}
	}
}

func (n *NFSTester) ReadTest() float64 {

	if n.filesWritten == 0 {
		fmt.Println("[error] Unable to perform ReadTest, no files written.")
		return float64(0)
	}
	atomic.StoreInt32(&n.atm_finished, 0)
	atomic.StoreUint64(&n.atm_counter_bytes_read, 0)

	for i := 1; i <= n.filesWritten; i++ {
		fname := generateTestFilename(i)
		n.wg.Add(1)
		go n.readOneFile(fname)
	}

	time.Sleep(nfsTestPeriod * time.Second)
	atomic.StoreInt32(&n.atm_finished, 1)
	n.wg.Wait()

	total_bytes := atomic.LoadUint64(&n.atm_counter_bytes_read)
	return float64(total_bytes) / float64(nfsTestPeriod)
}
