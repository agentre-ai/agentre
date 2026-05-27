package remote_device_svc_test

import (
	"encoding/json"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/service/remote_device_svc"
)

func TestRemoteDeviceSvc_ProviderCache_RecordAndList(t *testing.T) {
	Convey("ProviderCache", t, func() {
		_, _, _, _, svc := setupSvc(t)

		Convey("List returns nil before any Record", func() {
			So(svc.ListDeviceProviders(7), ShouldBeNil)
		})

		Convey("Record then List returns the same providers", func() {
			input := []remote_device_svc.ProviderSummary{
				{Key: "k-1", Name: "main", Type: "anthropic"},
				{Key: "k-2", Name: "backup", Type: "openai"},
			}
			svc.RecordDeviceProviders(7, input)

			got := svc.ListDeviceProviders(7)
			So(got, ShouldHaveLength, 2)
			So(got[0].Key, ShouldEqual, "k-1")
			So(got[1].Key, ShouldEqual, "k-2")
		})

		Convey("List for unknown deviceID returns nil", func() {
			svc.RecordDeviceProviders(7, []remote_device_svc.ProviderSummary{
				{Key: "k-1", Name: "main", Type: "anthropic"},
			})
			So(svc.ListDeviceProviders(99), ShouldBeNil)
		})

		Convey("Second Record for same deviceID overwrites", func() {
			svc.RecordDeviceProviders(7, []remote_device_svc.ProviderSummary{
				{Key: "k-1", Name: "old", Type: "anthropic"},
			})
			svc.RecordDeviceProviders(7, []remote_device_svc.ProviderSummary{
				{Key: "k-2", Name: "new", Type: "openai"},
			})
			got := svc.ListDeviceProviders(7)
			So(got, ShouldHaveLength, 1)
			So(got[0].Key, ShouldEqual, "k-2")
		})

		Convey("Record with empty list is distinguishable from never recorded", func() {
			svc.RecordDeviceProviders(7, []remote_device_svc.ProviderSummary{})

			got := svc.ListDeviceProviders(7)
			So(got, ShouldNotBeNil)
			So(got, ShouldHaveLength, 0)
		})

		Convey("List returns a defensive copy (mutation doesn't affect cache)", func() {
			svc.RecordDeviceProviders(7, []remote_device_svc.ProviderSummary{
				{Key: "k-1", Name: "main", Type: "anthropic"},
			})
			got := svc.ListDeviceProviders(7)
			got[0].Key = "mutated"
			// Second call returns original
			got2 := svc.ListDeviceProviders(7)
			So(got2[0].Key, ShouldEqual, "k-1")
		})

		Convey("concurrent Record+List is race-free (smoke)", func() {
			var wg sync.WaitGroup
			for i := 0; i < 20; i++ {
				wg.Add(2)
				devID := int64(i % 3)
				go func() {
					defer wg.Done()
					svc.RecordDeviceProviders(devID, []remote_device_svc.ProviderSummary{
						{Key: "k", Name: "n", Type: "t"},
					})
				}()
				go func() {
					defer wg.Done()
					_ = svc.ListDeviceProviders(devID)
				}()
			}
			wg.Wait()
		})
	})
}

func TestProviderSummary_JSONContract(t *testing.T) {
	Convey("ProviderSummary uses lower-camel JSON fields for Wails/frontend", t, func() {
		raw, err := json.Marshal(remote_device_svc.ProviderSummary{
			Key:  "k-1",
			Name: "Provider",
			Type: "anthropic",
		})

		So(err, ShouldBeNil)
		So(string(raw), ShouldEqual, `{"key":"k-1","name":"Provider","type":"anthropic"}`)
	})
}
