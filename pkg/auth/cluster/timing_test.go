package cluster_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"golang.org/x/sys/unix"
	"gonum.org/v1/gonum/stat"

	"github.com/onsi/gomega/gmeasure"
	"go.uber.org/zap/zapcore"

	corev1 "github.com/rancher/opni/pkg/apis/core/v1"
	"github.com/rancher/opni/pkg/auth/cluster"
	"github.com/rancher/opni/pkg/keyring"
	"github.com/rancher/opni/pkg/logger"
	"github.com/rancher/opni/pkg/storage"
	"github.com/rancher/opni/pkg/test"
)

func threadClock() int64 {
	var time unix.Timespec
	unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &time)
	return time.Nano()
}

var _ = Describe("Request Timing", Ordered, Label("unit", "slow", "temporal"), func() {
	var ctrl *gomock.Controller
	BeforeAll(func() {
		ctrl = gomock.NewController(GinkgoT())
		// temporarily pause garbage collection and debug logging to avoid interfering with timing
		gcPercent := debug.SetGCPercent(-1)
		logger.DefaultLogLevel.SetLevel(zapcore.ErrorLevel)
		DeferCleanup(func() {
			debug.SetGCPercent(gcPercent)
			logger.DefaultLogLevel.SetLevel(zapcore.DebugLevel)
		})
	})
	Specify("different unauthorized requests should take the same amount of time", func() {
		store := test.NewTestKeyringStore(ctrl, "", &corev1.Reference{
			Id: "cluster-1",
		})
		store.Put(context.Background(), keyring.New(keyring.NewSharedKeys(testSharedSecret)))
		handler := func(prefix string, ref *corev1.Reference) (storage.KeyringStore, error) {
			if ref.Id == "cluster-1" {
				return store, nil
			}
			return nil, errors.New("not found")
		}
		broker := test.NewTestKeyringStoreBroker(ctrl, handler)
		mw, err := cluster.New(context.Background(), broker, "X-Test")
		Expect(err).NotTo(HaveOccurred())

		exp := gmeasure.NewExperiment("request-timing")

		largeBody := make([]byte, 2*1024*1024)
		rand.Read(largeBody)
		largeBody2 := make([]byte, 2*1024*1024)
		rand.Read(largeBody2)
		invalidExists := invalidAuthHeader("cluster-1", largeBody2)
		validDoesNotExist := validAuthHeader("cluster-2", largeBody)
		invalidDoesNotExist := invalidAuthHeader("cluster-2", largeBody2)

		titleA := "A) valid mac, cluster does not exist"
		titleB := "B) valid mac, cluster exists"
		titleC := "C) invalid mac, cluster exists"

		threads := runtime.NumCPU()

		sampleTarget := 50000

		By(fmt.Sprintf("running tests in parallel using %d threads", threads))

		wg := sync.WaitGroup{}
		for t := 0; t < threads; t++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()

				for i := 0; i < sampleTarget/threads; i++ {
					{ // A
						start := threadClock()
						code, _, _ := mw.VerifyKeyring(validDoesNotExist, largeBody)
						duration := threadClock() - start
						exp.RecordDuration(titleA, time.Duration(duration), gmeasure.Precision(time.Nanosecond))
						Expect(code).To(Equal(http.StatusUnauthorized))
					}
					{ // B
						start := threadClock()
						code, _, _ := mw.VerifyKeyring(invalidDoesNotExist, largeBody)
						duration := threadClock() - start
						exp.RecordDuration(titleB, time.Duration(duration), gmeasure.Precision(time.Nanosecond))
						Expect(code).To(Equal(http.StatusUnauthorized))
					} // C
					{
						start := threadClock()
						code, _, _ := mw.VerifyKeyring(invalidExists, largeBody)
						duration := threadClock() - start
						exp.RecordDuration(titleC, time.Duration(duration), gmeasure.Precision(time.Nanosecond))
						Expect(code).To(Equal(http.StatusUnauthorized))
					}
				}
			}()
		}

		wg.Wait()

		By("computing results")

		a := exp.Get(titleA)
		b := exp.Get(titleB)
		c := exp.Get(titleC)

		aSamplesC := lo.Async(func() []float64 {
			s := lo.Map(a.Durations, durationToFloat)
			return sortAndRemoveOutliers(s, a.Stats())
		})
		bSamplesC := lo.Async(func() []float64 {
			s := lo.Map(b.Durations, durationToFloat)
			return sortAndRemoveOutliers(s, b.Stats())
		})
		cSamplesC := lo.Async(func() []float64 {
			s := lo.Map(c.Durations, durationToFloat)
			return sortAndRemoveOutliers(s, c.Stats())
		})

		aSamples := <-aSamplesC
		bSamples := <-bSamplesC
		cSamples := <-cSamplesC

		aScoreC := lo.Async(func() float64 { return ksTest(aSamples, bSamples) })
		bScoreC := lo.Async(func() float64 { return ksTest(bSamples, cSamples) })
		cScoreC := lo.Async(func() float64 { return ksTest(aSamples, cSamples) })

		aScore := <-aScoreC
		bScore := <-bScoreC
		cScore := <-cScoreC

		AddReportEntry(fmt.Sprintf("Score (A,B): %f", aScore))
		AddReportEntry(fmt.Sprintf("Score (B,C): %f", bScore))
		AddReportEntry(fmt.Sprintf("Score (A,C): %f", cScore))

		for i, m := range exp.Measurements {
			switch m.Name {
			case titleA:
				m.Durations = lo.Map(aSamples, floatToDuration)
			case titleB:
				m.Durations = lo.Map(bSamples, floatToDuration)
			case titleC:
				m.Durations = lo.Map(cSamples, floatToDuration)
			}
			exp.Measurements[i] = m
		}

		AddReportEntry(exp.Name, exp)

		Expect(aScore).To(BeNumerically("<", threshold), "Score (A,B)")
		Expect(bScore).To(BeNumerically("<", threshold), "Score (B,C)")
		Expect(cScore).To(BeNumerically("<", threshold), "Score (A,C)")
	})
})

var threshold = 0.2

func ksTest(a, b []float64) float64 {
	return stat.KolmogorovSmirnov(a, nil, b, nil)
}

func sortAndRemoveOutliers(data []float64, stats gmeasure.Stats) []float64 {
	sort.Float64s(data)
	stdDev := stats.FloatFor(gmeasure.StatStdDev)
	mean := stats.FloatFor(gmeasure.StatMean)
	lowerIdx := sort.SearchFloat64s(data, mean-3*stdDev)
	upperIdx := sort.SearchFloat64s(data, mean+3*stdDev)
	return data[lowerIdx:upperIdx]
}

func floatToDuration(f float64, _ int) time.Duration {
	return time.Duration(f)
}

func durationToFloat(d time.Duration, _ int) float64 {
	return float64(d)
}
