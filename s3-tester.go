package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

const s3TestPeriod = 60

type S3Tester struct {
	endpoint    string
	accessKey   string
	secretKey   string
	bucket      string
	concurrency int

	wg                        sync.WaitGroup
	atm_finished              int32
	atm_counter_bytes_written uint64
	atm_counter_bytes_read    uint64

	objectsWritten int
}

func NewS3Tester(endpoint string, accessKey string, secretKey string, bucketname string, concurrency int) (*S3Tester, error) {

	s3Tester := &S3Tester{endpoint: endpoint, accessKey: accessKey, secretKey: secretKey, bucket: bucketname, concurrency: concurrency, objectsWritten: 0}

	s3Config := &aws.Config{
		Endpoint:         aws.String(endpoint),
		Credentials:      credentials.NewStaticCredentials(accessKey, secretKey, ""),
		Region:           aws.String("us-east-1"),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
	}

	sess := session.Must(session.NewSession(s3Config))
	svc := s3.New(sess)

	count := 0
	err := svc.ListObjectsPages(&s3.ListObjectsInput{
		Bucket: &bucketname,
	}, func(p *s3.ListObjectsOutput, _ bool) (shouldContinue bool) {
		count += len(p.Contents)
		return true
	})
	if err != nil {
		fmt.Println("failed to list objects", err)
		return nil, err
	}
	if count != 0 {
		fmt.Printf("Expected zero objects in new bucket, found %d\n", count)
	}

	return s3Tester, err
}

func (s *S3Tester) writeOneObject(sname string) {

	defer s.wg.Done()
	src := make([]byte, 8*1024*1024)
	rand.Read(src)
	r := bytes.NewReader(src)

	s3Config := &aws.Config{
		Endpoint:         aws.String(s.endpoint),
		Credentials:      credentials.NewStaticCredentials(s.accessKey, s.secretKey, ""),
		Region:           aws.String("us-east-1"),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
	}

	sess := session.Must(session.NewSession(s3Config))
	svc := s3manager.NewUploader(sess)

	bytes_written := uint64(0)

	for atomic.LoadInt32(&s.atm_finished) == 0 {

		_, err := svc.Upload(&s3manager.UploadInput{
			Bucket: &s.bucket,
			Key:    &sname,
			Body:   r,
		})
		if err != nil {
			fmt.Println("error", err)
		}
		bytes_written += uint64(len(src))
	}

	atomic.AddUint64(&s.atm_counter_bytes_written, bytes_written)
}

func generateTestObjectName(i int) string {

	baseDir := "/"
	oname := baseDir + "objname" + strconv.Itoa(i)
	return oname
}

func (s *S3Tester) WriteTest() float64 {

	atomic.StoreInt32(&s.atm_finished, 0)
	atomic.StoreUint64(&s.atm_counter_bytes_written, 0)

	for i := 1; i <= s.concurrency; i++ {
		prefix := generateTestObjectName(i)
		s.wg.Add(1)
		go s.writeOneObject(prefix)
	}

	time.Sleep(s3TestPeriod * time.Second)
	atomic.StoreInt32(&s.atm_finished, 1)
	s.wg.Wait()
	s.objectsWritten += s.concurrency

	total_bytes := atomic.LoadUint64(&s.atm_counter_bytes_written)
	return float64(total_bytes) / float64(s3TestPeriod)
}

func (s *S3Tester) readOneObject(prefix string) {

	defer s.wg.Done()
	s3Config := &aws.Config{
		Endpoint:         aws.String(s.endpoint),
		Credentials:      credentials.NewStaticCredentials(s.accessKey, s.secretKey, ""),
		Region:           aws.String("us-east-1"),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
	}

	sess := session.Must(session.NewSession(s3Config))
	downloader := s3manager.NewDownloader(sess)

	nullSink := newNullWriterAt()

	for atomic.LoadInt32(&s.atm_finished) == 0 {

		_, err := downloader.Download(nullSink, &s3.GetObjectInput{
			Bucket: &s.bucket,
			Key:    &prefix,
		})
		if err != nil {
			fmt.Println("failed to download object, %v", err)
		}
	}
	atomic.AddUint64(&s.atm_counter_bytes_read, nullSink.bytesRead)
}

func (s *S3Tester) ReadTest() float64 {

	if s.objectsWritten == 0 {
		fmt.Println("[error] Unable to perform S3 ReadTest, no objects written.")
		return float64(0)
	}
	atomic.StoreInt32(&s.atm_finished, 0)
	atomic.StoreUint64(&s.atm_counter_bytes_read, 0)

	for i := 1; i <= s.objectsWritten; i++ {
		prefix := generateTestObjectName(i)
		s.wg.Add(1)
		go s.readOneObject(prefix)
	}

	time.Sleep(s3TestPeriod * time.Second)
	atomic.StoreInt32(&s.atm_finished, 1)
	s.wg.Wait()

	total_bytes := atomic.LoadUint64(&s.atm_counter_bytes_read)
	return float64(total_bytes) / float64(s3TestPeriod)
}
