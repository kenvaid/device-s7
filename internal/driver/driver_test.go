package driver

import (
	"reflect"
	"testing"

	sdkModel "github.com/edgexfoundry/device-sdk-go/v4/pkg/models"
	"github.com/edgexfoundry/go-mod-core-contracts/v4/clients/logger"
)

func TestDriver_getDBInfo(t *testing.T) {
	type fields struct {
		lc        logger.LoggingClient
		asyncCh   chan<- *sdkModel.AsyncValues
		s7Clients map[string]*S7Client
	}
	type args struct {
		variable string
	}
	var driver fields
	driver.lc = logger.NewClient("S7", "Error")
	tests := []struct {
		name       string
		fields     *fields
		args       args
		wantDbInfo *DBInfo
		wantErr    bool
	}{
		{
			name:       "invalid address-DB1.DBX100",
			fields:     &driver,
			args:       args{variable: "DB1.DBX100"},
			wantDbInfo: nil,
			wantErr:    true,
		},
		{
			name:       "invalid address-DB1.DBX86.2.1",
			fields:     &driver,
			args:       args{variable: "DB1.DBX86.2.1"},
			wantDbInfo: nil,
			wantErr:    true,
		},
		{
			name:       "invalid address-DBX100",
			fields:     &driver,
			args:       args{variable: "DBX100"},
			wantDbInfo: nil,
			wantErr:    true,
		},
		{
			name:   "valid address-DB1.DBX100.0",
			fields: &driver,
			args:   args{variable: "DB1.DBX100.0"},
			wantDbInfo: &DBInfo{
				Area:       0x84,
				DBNumber:   1,
				Start:      100,
				Amount:     1,
				WordLength: s7wlbit,
				DBArray:    []string{"DB1", "DBX100", "0"},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Driver{
				lc:        tt.fields.lc,
				asyncCh:   tt.fields.asyncCh,
				s7Clients: tt.fields.s7Clients,
			}
			gotDbInfo, err := s.getDBInfo(tt.args.variable)
			if (err != nil) != tt.wantErr {
				t.Errorf("getDBInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotDbInfo, tt.wantDbInfo) {
				t.Errorf("getDBInfo() gotDbInfo = %v, want %v", gotDbInfo, tt.wantDbInfo)
			}
		})
	}
}

func TestS7AddressByteAndBitContract(t *testing.T) {
	s := &Driver{lc: logger.NewClient("S7", "Error")}
	for _, tc := range []struct {
		address              string
		area, db, start, bit int
	}{
		{"DB1.DBX100.3", 0x84, 1, 100, 3}, {"M100.3", 0x83, 0, 100, 3},
		{"I100.3", 0x81, 0, 100, 3}, {"Q100.3", 0x82, 0, 100, 3},
		{"V100.3", 0x84, 1, 100, 3}, {"DB65535.DBX40000.7", 0x84, 65535, 40000, 7},
		{"EB100", 0x81, 0, 100, 0}, {"AB100", 0x82, 0, 100, 0}, {"MB100", 0x83, 0, 100, 0},
		{"C100", 0x1c, 0, 100, 0}, {"T100", 0x1d, 0, 100, 0},
	} {
		info, err := s.getDBInfo(tc.address)
		if err != nil || info.Area != tc.area || info.DBNumber != tc.db || info.Start != tc.start || info.Bit != tc.bit {
			t.Fatalf("%s: %+v %v", tc.address, info, err)
		}
	}
	for _, address := range []string{"", "D", "DB1.X", "DB1.DBX0", "DB1.DBX0.8", "DB-1.DBB0", "M0.8", "I-1.0", "Q0.-1", "DB65536.DBB0", "DB1.DBB2097152", "DB1.DBW2097151", "DB1.DBB0.0", "IB", "C65536"} {
		if _, err := s.getDBInfo(address); err == nil {
			t.Fatalf("invalid address accepted: %s", address)
		}
	}
}
