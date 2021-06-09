package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
)

const testFilesystemName = "deleteme-go-plumbing"
const testObjectAccountName = "deleteme-go-plumb-account"
const testObjectUserName = "deleteme-go-plumb-user"
const testObjectBucketName = "deleteme-go-plumb-bucket"


func main() {

	skipNfsPtr := flag.Bool("skip-nfs", false, "Skip NFS Tests")
	skipS3Ptr := flag.Bool("skip-s3", false, "Skip S3 Tests")
	flag.Parse()

	mgmtVIP := os.Getenv("FB_MGMT_VIP")
	fbtoken := os.Getenv("FB_TOKEN")

	if mgmtVIP == "" {
		fmt.Println("Must set environment variable FB_MGMT_VIP to FlashBlade management VIP.")
		os.Exit(1)
	}
	if fbtoken == "" {
		fmt.Println("Must set environment variable FB_TOKEN to FlashBlade REST Token.")
		os.Exit(1)
	}

	coreCount := runtime.NumCPU()
	if coreCount < 12 {
		fmt.Printf("WARNING. Found %d cores, recommend at least 12 cores to prevent client bottlenecks.\n", coreCount)
	}

    hostname := getShortHostname()

	c, err := NewFlashBladeClient(mgmtVIP, fbtoken)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer c.Close()

	nets, err := c.ListNetworkInterfaces()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	dataVips := make(map[string][]string)
	for _, net := range nets {
		for i := range net.Services {
			if net.Services[i] == "data" {
				dataVips[net.Subnet.Name] = append(dataVips[net.Subnet.Name], net.Address)
			}
		}
	}

	if len(dataVips) == 0 {
		fmt.Println("Found no data VIPs, unable to proceed.")
		os.Exit(1)
	}
	fmt.Printf("Found %d subnets with data VIPs. Will test one VIP per subnet.\n", len(dataVips))

	var results []string

	// ===== NFS Tests =====
	if *skipNfsPtr == false {

		fsname := testFilesystemName + "-" + hostname
		fmt.Printf("Checking for filesystem %s\n", fsname)
		_, err = c.GetFileSystem(fsname)
		if err == nil {
			fmt.Printf("Filesystem %s already exists, exiting\n", fsname)
			fmt.Println(err)
			os.Exit(1)
		}

		for k, v := range dataVips {

			fs := FileSystem{Name: fsname}
			fs.Nfs.Enabled = true
			fs.Nfs.V3Enabled = true

			err = c.CreateFileSystem(fs)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			dataVip := v[0]
			fmt.Printf("Found %d data VIPs in subnet %s, will use: %s\n", len(v), k, dataVip)

			export := "/" + fsname
			fmt.Printf("Mounting NFS export %s at %s\n", export, dataVip)
			nfs, err := NewNFSTester(dataVip, export, coreCount*2)

			if err != nil {
				fmt.Println(err)
				c.DeleteFileSystem(fsname)
				results = append(results, fmt.Sprintf("%s,nfs,MOUNT FAILED,-,-", dataVip))
				continue
			}

			fmt.Println("Running NFS write test.")
			write_bytes_per_sec := nfs.WriteTest()
			fmt.Printf("Write Throughput = %s\n", ByteRateSI(write_bytes_per_sec))

			fmt.Println("Running NFS read test.")
			read_bytes_per_sec := nfs.ReadTest()
			fmt.Printf("Read Throughput = %s\n", ByteRateSI(read_bytes_per_sec))

			results = append(results, fmt.Sprintf("%s,nfs,SUCCESS,%s,%s", dataVip, ByteRateSI(write_bytes_per_sec), ByteRateSI(read_bytes_per_sec)))

			err = c.DeleteFileSystem(fsname)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		}
	}

	// ===== S3 Tests =====
	if *skipS3Ptr == false {
		objAccountName := testObjectAccountName + "-" + hostname
		fmt.Printf("Creating object store account %s\n", objAccountName)
		err = c.CreateObjectStoreAccount(objAccountName)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		objUserName := testObjectUserName + "-" + hostname
		fmt.Printf("Creating object store user %s\n", objUserName)
		err = c.CreateObjectStoreUser(objUserName, objAccountName)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		fmt.Printf("Creating object store access keys for %s\n", objUserName)
		keys, err := c.CreateObjectStoreAccessKeys(objUserName, objAccountName)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		for k, v := range dataVips {
			dataVip := v[0]
			fmt.Printf("Found %d data VIPs in subnet %s, will use: %s\n", len(v), k, dataVip)

			bucketName := testObjectBucketName + "-" + hostname
			err = c.CreateObjectStoreBucket(bucketName, objAccountName)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			s3, err := NewS3Tester(dataVip, keys[0].Name, keys[0].SecretAccessKey, bucketName, coreCount)
			if err != nil {
				fmt.Println(err)
				c.DeleteObjectStoreBucket(bucketName)
				results = append(results, fmt.Sprintf("%s,s3,FAILED TO CONNECT,-,-", dataVip))
				continue
			}

			fmt.Println("Running S3 write test.")
			write_bytes_per_sec := s3.WriteTest()
			fmt.Printf("Write Throughput = %s\n", ByteRateSI(write_bytes_per_sec))

			fmt.Println("Running S3 read test.")
			read_bytes_per_sec := s3.ReadTest()
			fmt.Printf("Read Throughput = %s\n", ByteRateSI(read_bytes_per_sec))

			results = append(results, fmt.Sprintf("%s,s3,SUCCESS,%s,%s", dataVip, ByteRateSI(write_bytes_per_sec), ByteRateSI(read_bytes_per_sec)))

			err = c.DeleteObjectStoreBucket(bucketName)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		}

		fmt.Printf("Deleting object store keys %s\n", keys[0].Name)
		err = c.DeleteObjectStoreAccessKey(keys[0].Name)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		fmt.Printf("Deleting object store user %s\n", objUserName)
		err = c.DeleteObjectStoreUser(objUserName, objAccountName)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		fmt.Printf("Deleting object store account %s\n", objAccountName)
		err = c.DeleteObjectStoreAccount(objAccountName)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}

	fmt.Println()
	fmt.Println("dataVip,protocol,result,write_tput,read_tput")
	for _, r := range results {
		fmt.Println(r)
	}
}
