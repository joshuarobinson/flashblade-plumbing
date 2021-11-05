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
	testDurationPtr := flag.Int("duration", 60, "Duration to run each test, in seconds.")
	dataVipPtr := flag.String("datavip", "", "Remote IP address for data connections.")
	filesystemPtr := flag.String("filesystem", "", "Remote filesystem for NFS testing. Default is to automatically create temporary filesystem.")
	bucketPtr := flag.String("bucket", "", "Remote bucket for S3 testing. Default is to automatically create temporary bucket.")
	flag.Parse()

	testDuration := *testDurationPtr

	mgmtVIP := os.Getenv("FB_MGMT_VIP")
	fbtoken := os.Getenv("FB_TOKEN")

	hostname := getShortHostname()

	fsName := *filesystemPtr
	bucketName := *bucketPtr

	// If either filesystem or bucket manually specified, disable autoprovisioning.
	autoProvision := fsName == "" && bucketName == ""

	if !autoProvision && *dataVipPtr == "" && fsName != "" {
		fmt.Println("ERROR. If testing an existing filesystem, must also specifiy --datavip option")
		os.Exit(1)
	}

	if autoProvision {
		fsName = testFilesystemName + "-" + hostname
		bucketName = testObjectBucketName + "-" + hostname
	}

	// Infer "skip" if either of the bucket or filesystem wasn't specified.
	if !autoProvision {
		*skipNfsPtr = *skipNfsPtr || fsName == ""
		*skipS3Ptr = *skipS3Ptr || bucketName == ""
	}

	if autoProvision && mgmtVIP == "" {
		fmt.Println("ERROR. Must set environment variable FB_MGMT_VIP to FlashBlade management VIP.")
		os.Exit(1)
	}
	if autoProvision && fbtoken == "" {
		fmt.Println("ERROR. Must set environment variable FB_TOKEN to FlashBlade REST Token.")
		os.Exit(1)
	}

	coreCount := runtime.NumCPU()
	if coreCount < 12 {
		fmt.Printf("WARNING. Found %d cores, recommend at least 12 cores to prevent client bottlenecks.\n", coreCount)
	}

	// Begin Main application logic.
	var c *FlashBladeClient
	var err error

	if autoProvision {
		c, err = NewFlashBladeClient(mgmtVIP, fbtoken)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		defer c.Close()
	}

	var dataVips []string
	if *dataVipPtr != "" {
		dataVips = []string{*dataVipPtr}
	} else {
		dataVips, err = c.GetOneDataInterfacePerSubnet()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}

	if len(dataVips) > 1 {
		fmt.Printf("Found %d subnets with data VIPs. Will test one VIP per subnet.\n", len(dataVips))
	}

	if len(dataVips) == 0 {
		fmt.Println("Found no data VIPs, unable to proceed.")
		os.Exit(1)
	}

	var results []string

	// ===== NFS Tests =====
	if *skipNfsPtr == false {

		for _, dataVip := range dataVips {

			if autoProvision {
				fs := FileSystem{Name: fsName}
				fs.Nfs.Enabled = true
				fs.Nfs.V3Enabled = true

				fmt.Println("Creating filesystem ", fsName)
				err = c.CreateFileSystem(fs)
				if err != nil {
					fmt.Println(err)
					os.Exit(1)
				}
			}

			export := "/" + fsName
			fmt.Printf("Mounting NFS export %s at %s\n", export, dataVip)
			nfs, err := NewNFSTester(dataVip, export, coreCount*2, testDuration)

			if err != nil {
				fmt.Println(err)
				if autoProvision {
					c.DeleteFileSystem(fsName)
				}
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

			if autoProvision {
				err = c.DeleteFileSystem(fsName)
				if err != nil {
					fmt.Println(err)
					os.Exit(1)
				}
			}
		}
	}

	// ===== S3 Tests =====
	if *skipS3Ptr == false {

		objAccountName := testObjectAccountName + "-" + hostname
		objUserName := testObjectUserName + "-" + hostname
		accessKey := ""
		secretKey := ""

		if autoProvision {

			fmt.Printf("Creating object store account %s\n", objAccountName)
			err := c.CreateObjectStoreAccount(objAccountName)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

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
			accessKey = keys[0].Name
			secretKey = keys[0].SecretAccessKey
		}

		for _, dataVip := range dataVips {

			if autoProvision {
				err = c.CreateObjectStoreBucket(bucketName, objAccountName)
				if err != nil {
					fmt.Println(err)
					os.Exit(1)
				}
			}

			s3, err := NewS3Tester(dataVip, accessKey, secretKey, bucketName, coreCount, testDuration)
			if err != nil {
				fmt.Println(err)
				if autoProvision {
					c.DeleteObjectStoreBucket(bucketName)
				}
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

			if autoProvision {
				err = c.DeleteObjectStoreBucket(bucketName)
				if err != nil {
					fmt.Println(err)
					os.Exit(1)
				}
			}
		}

		if autoProvision {
			fmt.Printf("Deleting object store keys %s\n", accessKey)
			err = c.DeleteObjectStoreAccessKey(accessKey)
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
	}

	fmt.Println("\ndataVip,protocol,result,write_tput,read_tput")
	for _, r := range results {
		fmt.Println(r)
	}
}
