// -*- Mode: Go; indent-tabs-mode: t -*-
//
// Copyright (C) 2023 YIQISOFT
//
// SPDX-License-Identifier: Apache-2.0

// This package provides an example implementation of
// S7 interface.
package driver

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/edgexfoundry/device-sdk-go/v4/pkg/interfaces"
	sdkModel "github.com/edgexfoundry/device-sdk-go/v4/pkg/models"
	"github.com/edgexfoundry/go-mod-core-contracts/v4/clients/logger"
	"github.com/edgexfoundry/go-mod-core-contracts/v4/common"
	"github.com/edgexfoundry/go-mod-core-contracts/v4/models"

	"github.com/kenvaid/gos7"
	"github.com/spf13/cast"
)

const (
	// Word Length
	s7wlbit     = 0x01 // Bit (inside a word)
	s7wlbyte    = 0x02 // Byte (8 bit)
	s7wlChar    = 0x03 // Char (8 bit)
	s7wlword    = 0x04 // Word (16 bit)
	s7wlint     = 0x05 // Int (16 bit)
	s7wldword   = 0x06 // Double Word (32 bit)
	s7wldint    = 0x07 // DInt (32 bit)
	s7wlreal    = 0x08 // Real (32 bit float)
	s7wlcounter = 0x1C // Counter (16 bit)
	s7wltimer   = 0x1D // Timer (16 bit)
)

var once sync.Once
var driver *Driver

type Driver struct {
	lc        logger.LoggingClient
	asyncCh   chan<- *sdkModel.AsyncValues
	s7Clients map[string]*S7Client
	mu        sync.Mutex
}

func NewProtocolDriver() interfaces.ProtocolDriver {
	once.Do(func() {
		driver = new(Driver)
	})
	return driver
}

type CommandInfo struct {
	Host            string
	Port            int
	Rack            int
	Slot            int
	AddressType     string
	DbAddress       int
	StartingAddress int
	Length          int
	Pos             int
	ValueType       string
}

type DBInfo struct {
	Area       int
	DBNumber   int
	Start      int
	Bit        int
	Amount     int
	WordLength int
	DBArray    []string
}

// Initialize performs protocol-specific initialization for the device
// service.
func (s *Driver) Initialize(sdk interfaces.DeviceServiceSDK) error {
	s.lc = sdk.LoggingClient()
	s.asyncCh = sdk.AsyncValuesChannel()
	s.s7Clients = make(map[string]*S7Client)

	// initialize the all devices connection in the service started
	// for _, device := range sdk.Devices() {
	// 	s7Client := s.NewS7Client(device.Name, device.Protocols)
	// 	if s7Client == nil {
	// 		s.lc.Errorf("failed to initialize S7 client for '%s' device, skipping this device.", device.Name)
	// 		continue
	// 	}
	// 	s.s7Clients[device.Name] = s7Client
	// 	s.lc.Debugf("S7Client connected for device: %s", device.Name)
	// }

	return nil
}

func (s *S7Client) useClient() {
	s.mu.Lock()
}

func (s *S7Client) freeClient() {
	s.mu.Unlock()
}

// HandleReadCommands triggers a protocol Read operation for the specified device.
func (s *Driver) HandleReadCommands(deviceName string, protocols map[string]models.ProtocolProperties, reqs []sdkModel.CommandRequest) (res []*sdkModel.CommandValue, err error) {
	s.lc.Debugf("Driver.HandleReadCommands: protocols: %v, resource: %v, attributes: %v", protocols, reqs[0].DeviceResourceName, reqs[0].Attributes)

	// assume the max batch size is 16, must be less than 20
	var batch_size = 16

	var reqs_len = len(reqs)
	var s7DataItems = []gos7.S7DataItem{}

	var s7_errors = make([]string, reqs_len)
	// two-dimensional array for handle S7DataItems
	var dataset = make([][]byte, reqs_len)
	for i := range dataset {
		dataset[i] = make([]byte, 4) // 4 bytes
	}

	// Get S7 device connection information, each Device has its own connection.
	s7Client := s.getS7Client(deviceName, protocols)
	if s7Client == nil {
		return nil, fmt.Errorf("failed to get S7 client for '%s' device", deviceName)
	}
	s7Client.useClient()
	defer s7Client.freeClient()

	// assemble s7DataItems, get items, fetch data

	var times = int(reqs_len / batch_size)
	var remains = reqs_len % batch_size
	var tmp_reqs []sdkModel.CommandRequest

outloop:
	for j := 0; j <= times; j++ {

		// 1. init array
		s7DataItems = s7DataItems[:0] // clear array
		tmp_reqs = tmp_reqs[:0]       // clear array
		if j >= times {
			if remains > 0 {
				tmp_reqs = reqs[times*batch_size : times*batch_size+remains]
			} else {
				_ = tmp_reqs
				break outloop // load complete, exit loop
			}
		} else {
			tmp_reqs = reqs[j*batch_size : j*batch_size+batch_size]
		}

		// 2. get resources from reqs append to items
		var count int
		for i, req := range tmp_reqs {

			nodename := cast.ToString(req.Attributes["NodeName"])
			dbInfo, err := s.getDBInfo(nodename)
			if err != nil {
				count++
				s.lc.Errorf("device: %s convert nodeName to dbInfo failed,err =%v", deviceName, err)
				var nilS7DataItem = gos7.S7DataItem{
					Area:     0,
					WordLen:  0,
					DBNumber: 0,
					Start:    0,
					Bit:      0,
					Amount:   0,
					Data:     dataset[j*batch_size+i],
				}
				s7DataItems = append(s7DataItems, nilS7DataItem)
				continue
			}
			var s7DataItem = gos7.S7DataItem{
				Area:     dbInfo.Area,
				WordLen:  dbInfo.WordLength,
				DBNumber: dbInfo.DBNumber,
				Start:    dbInfo.Start,
				Bit:      dbInfo.Bit,
				Amount:   dbInfo.Amount,
				Data:     dataset[j*batch_size+i],
			}
			s7DataItems = append(s7DataItems, s7DataItem)
		}
		s.lc.Debugf("Read from S7DataItems for device: %s: %+v", deviceName, s7DataItems)
		if count == len(tmp_reqs) {
			s.lc.Errorf("device: %s commandRequest %+v is invalid", deviceName, tmp_reqs)
			continue
		}

		// 3. use AGReadMulti api to get values from S7 device, if error, try 3 times
		retrytimes := 3
		for {
			err = s7Client.Client.AGReadMulti(s7DataItems, len(s7DataItems))
			if err != nil {
				s.lc.Errorf("device: %s AGReadMulti Error: %s retries: %d", deviceName, err, retrytimes)
			} else {
				s.lc.Debugf("device: %s AGReadMulti read from 'dataset': ", deviceName, dataset)
				break
			}

			retrytimes--
			if retrytimes == 0 {
				s.lc.Errorf("S7 client for device %s has errors, closing client", deviceName)
				s.closeS7Client(deviceName)
				break outloop
			}
			time.Sleep(50 * time.Millisecond)
		}
		//4. use s7_errors to record the abnormal error messages of all read points
		for i, s7DataItem := range s7DataItems {
			if s7_error := s7DataItem.Error; s7_error != "" {
				s.lc.Errorf("device: %s s7DataItem:%+v,error: %s", deviceName, s7DataItem, s7_error)
				s7_errors[j*batch_size+i] = s7_error
			}
		}

	}
	// end assemble s7DataItems
	s.lc.Debugf("Read S7DataItems for device: %s: %+v", deviceName, s7DataItems)
	// read results from the dataset of s7DataItems
	for i, req := range reqs {

		var result *sdkModel.CommandValue
		var value any

		if s7_error := s7_errors[i]; s7_error != "" {
			s.lc.Errorf("S7 Client for device: %s AGRead req %+v failed,error: %s", deviceName, req, s7_error)
			continue
		}

		value, err := getCommandValueType(dataset[i], req.Type)
		if err != nil {
			s.lc.Errorf("device: %s getCommandValueType error: %s", deviceName, err)
			continue
		}

		result, err = getCommandValue(req, value)
		if err != nil {
			s.lc.Errorf("device: %s getCommandValue error: %v", deviceName, err)
			continue
		}

		res = append(res, result)
	}
	if len(res) == 0 {
		// s.mu.Lock()
		// s.closeS7Client(s7Client)
		// s.s7Clients[deviceName] = nil
		// s.mu.Unlock()
		// s.lc.Errorf("read reqs %+v failed", reqs)
		return nil, fmt.Errorf("device: %s read reqs %+v failed", deviceName, reqs)
	}
	s.lc.Debugf("CommandValues for device %s: %s", deviceName, res)

	return
}

// HandleWriteCommands passes a slice of CommandRequest struct each representing
// a ResourceOperation for a specific device resource.
// Since the commands are actuation commands, params provide parameters for the individual
// command.
func (s *Driver) HandleWriteCommands(deviceName string, protocols map[string]models.ProtocolProperties, reqs []sdkModel.CommandRequest,
	params []*sdkModel.CommandValue) error {
	s.lc.Debugf("Driver.HandleWriteCommands: protocols: %v, resource: %v, parameters: %v", protocols, reqs[0].DeviceResourceName, params)

	var err error

	// assume the max batch size is 16, must be less than 20
	var batch_size = 16

	var reqs_len = len(reqs)
	var s7DataItems = []gos7.S7DataItem{}
	var helper gos7.Helper

	var s7_errors = make([]string, reqs_len)
	// two-dimensional array for handle S7DataItems
	var dataset = make([][]byte, reqs_len)
	for i := range dataset {
		dataset[i] = make([]byte, 4) // 4 bytes
	}

	// transfer command values to S7DataItems
	var count int
	for i, req := range reqs {

		s.lc.Debugf("S7Driver.HandleWriteCommands: device: %s, protocols: %v, resource: %v, parameters: %v, attributes: %v", deviceName, protocols, req.DeviceResourceName, params[i], req.Attributes)

		var nodeName = cast.ToString(req.Attributes["NodeName"])
		var dbInfo, err = s.getDBInfo(nodeName)
		if err != nil {
			count++
			s.lc.Errorf("device: %s convert nodeName %v to dbInfo failed,err =%v", deviceName, nodeName, err)
			dbInfo = &DBInfo{
				Area:       0,
				DBNumber:   0,
				Start:      0,
				Bit:        0,
				Amount:     0,
				WordLength: 0,
				DBArray:    []string{nodeName},
			}
		}

		reading, err := newCommandValue(req.Type, params[i])
		if err != nil {
			s.lc.Errorf("device: %s newCommandValue error: %s", deviceName, err)
		}
		helper.SetValueAt(dataset[i], 0, reading)

		// create gos7 DataItem
		var s7DataItem = gos7.S7DataItem{
			Area:     dbInfo.Area,
			WordLen:  dbInfo.WordLength,
			DBNumber: dbInfo.DBNumber,
			Start:    dbInfo.Start,
			Bit:      dbInfo.Bit,
			Amount:   dbInfo.Amount,
			Data:     dataset[i],
		}
		s7DataItems = append(s7DataItems, s7DataItem)

	}
	if count == len(reqs) {
		// s.lc.Errorf("commandWrite %+v is invalid", reqs)
		return fmt.Errorf("device: %s commandWrite %+v is invalid", deviceName, reqs)
	}
	s.lc.Debugf("Write to S7DataItems for device %s: %s", deviceName, s7DataItems)

	// send command requests
	times := int(reqs_len / batch_size)
	remains := reqs_len % batch_size
	var tmp_s7DateItems = []gos7.S7DataItem{}

	s7Client := s.getS7Client(deviceName, protocols)
	if s7Client == nil {
		return fmt.Errorf("failed to get S7 client for '%s' device", deviceName)
	}
	s7Client.useClient()
	defer s7Client.freeClient()

outloop:
	for j := 0; j <= times; j++ {

		tmp_s7DateItems = tmp_s7DateItems[:0] // clear array
		if j >= times {
			if remains > 0 {
				tmp_s7DateItems = s7DataItems[times*batch_size : times*batch_size+remains]
			} else {
				_ = tmp_s7DateItems
				break outloop // load complete, exit loop
			}
		} else {
			tmp_s7DateItems = s7DataItems[j*batch_size : j*batch_size+batch_size]
		}

		// write data to S7 device, if error, try 3 times
		retrytimes := 3
		for {
			err = s7Client.Client.AGWriteMulti(tmp_s7DateItems, len(tmp_s7DateItems))
			if err != nil {
				s.lc.Errorf("device: %s AGWriteMulti Error: %s", deviceName, err)
			} else {
				s.lc.Debugf("device: %s AGWriteMulti write from 'dataset': %s", deviceName, dataset)
				break
			}

			retrytimes--
			if retrytimes == 0 {
				s.lc.Errorf("S7 client for device %s has errors, closing client", deviceName)
				s.closeS7Client(deviceName)
				break outloop
			}
		}
		// Record all errors
		for i, tmp_s7DataItem := range tmp_s7DateItems {
			if s7_error := tmp_s7DataItem.Error; s7_error != "" {
				s.lc.Errorf("device: %s tmp_s7DataItem:%+v,error: %s", deviceName, tmp_s7DataItem, s7_error)
				s7_errors[j*batch_size+i] = s7_error
			}
		}

	}

	for _, s7_error := range s7_errors {
		if s7_error != "" {
			s.lc.Errorf("S7 Client for device %s AGWriteMulti error: %s", deviceName, s7_error)
			return err
		}
	}

	return nil
}

// Stop the protocol-specific DS code to shutdown gracefully, or
// if the force parameter is 'true', immediately. The driver is responsible
// for closing any in-use channels, including the channel used to send async
// readings (if supported).
func (s *Driver) Stop(force bool) error {

	s.mu.Lock()
	s.s7Clients = nil
	s.mu.Unlock()

	// Then Logging Client might not be initialized
	if s.lc != nil {
		s.lc.Debugf("Driver.Stop called: force=%v", force)
	}
	return nil
}

// AddDevice is a callback function that is invoked
// when a new Device associated with this Device Service is added
func (s *Driver) AddDevice(deviceName string, protocols map[string]models.ProtocolProperties, adminState models.AdminState) error {
	s.lc.Debugf("a new Device is added: %s", deviceName)

	s.closeS7Client(deviceName)

	s7Client := s.getS7Client(deviceName, protocols)
	if s7Client == nil {
		errt := fmt.Errorf("failed to initialize S7 client for '%s' device, skipping this device", deviceName)
		s.lc.Errorf(errt.Error())
		return errt
	}
	s.mu.Lock()
	s.s7Clients[deviceName] = s7Client
	s.mu.Unlock()
	return nil
}

// UpdateDevice is a callback function that is invoked
// when a Device associated with this Device Service is updated
func (s *Driver) UpdateDevice(deviceName string, protocols map[string]models.ProtocolProperties, adminState models.AdminState) error {
	s.lc.Debugf("Device %s is updated", deviceName)

	s7Client := s.NewS7Client(deviceName, protocols)
	if s7Client == nil {
		errt := fmt.Errorf("failed to initialize S7 client for '%s' device, skipping this device", deviceName)
		s.lc.Errorf(errt.Error())
		return errt
	}
	s.mu.Lock()
	s.s7Clients[deviceName] = s7Client
	s.mu.Unlock()

	return nil
}

// RemoveDevice is a callback function that is invoked
// when a Device associated with this Device Service is removed
func (s *Driver) RemoveDevice(deviceName string, protocols map[string]models.ProtocolProperties) error {
	s.lc.Debugf("Device %s is removed", deviceName)
	s.mu.Lock()
	delete(s.s7Clients, deviceName)
	s.mu.Unlock()
	return nil
}

func (s *Driver) ValidateDevice(device models.Device) error {
	s.lc.Debugf("Validating device: %s", device.Name)

	protocols := device.Protocols
	pp := protocols[Protocol]
	var errt error

	if pp == nil {
		errt = fmt.Errorf("%s not found in protocols for device %s", Protocol, device.Name)
		s.lc.Error(errt.Error())
		return errt
	}

	_, errt = cast.ToStringE(pp["Host"])
	if errt != nil {
		s.lc.Errorf("Host not found or not a string in Protocol, error: %s", errt)
		return errt
	}
	_, errt = cast.ToIntE(pp["Port"])
	if errt != nil {
		s.lc.Errorf("Port not found or not an integer in Protocol, error: %s", errt)
		return errt
	}
	_, errt = cast.ToIntE(pp["Rack"])
	if errt != nil {
		s.lc.Errorf("Rack not found or not an integer in Protocol, error: %s", errt)
		return errt
	}
	_, errt = cast.ToIntE(pp["Slot"])
	if errt != nil {
		s.lc.Errorf("Slot not found or not an integer in Protocol, error: %s", errt)
		return errt
	}
	_, errt = cast.ToIntE(pp["Timeout"])
	if errt != nil {
		s.lc.Errorf("Timeout not found or not an integer in Protocol, USE DEFAULT 30s error: %s", errt)
		pp["Timeout"] = 30
	}
	_, errt = cast.ToIntE(pp["IdleTimeout"])
	if errt != nil {
		s.lc.Errorf("IdleTimeout not found or not an ingeger in Protocol, USE DEFAULT 30s, error: %s", errt)
		pp["IdleTimeout"] = 30
	}

	return nil
}

// Create S7Client by 'Device' definition
func (s *Driver) NewS7Client(deviceName string, protocol map[string]models.ProtocolProperties) *S7Client {

	pp := protocol[Protocol]

	host, _ := cast.ToStringE(pp["Host"])
	port, _ := cast.ToStringE(pp["Port"])
	rack, _ := cast.ToIntE(pp["Rack"])
	slot, _ := cast.ToIntE(pp["Slot"])
	timeout, _ := cast.ToIntE(pp["Timeout"])
	idletimeout, _ := cast.ToIntE(pp["IdleTimeout"])

	// create handler: PLC tcp client
	address := host + ":" + port
	handler := gos7.NewTCPClientHandler(address, rack, slot)
	if handler == nil {
		s.lc.Errorf("Cant not create TCPClientHandler: %s", deviceName)
		return nil
	}
	s.lc.Debugf("New TCP Client for %s: %s", deviceName, address)

	// handler connect timeout from 'Timeout'
	handler.Timeout = time.Duration(timeout) * time.Second

	// handler connect idle timeout from 'IdleTimeout'
	handler.IdleTimeout = time.Duration(idletimeout) * time.Second

	// connect to S7
	err := handler.Connect()
	if err != nil {
		s.lc.Errorf("Can't handler S7 Connect: %s, error: %s", deviceName, err)
		return nil
	}

	s7client := gos7.NewClient(handler)
	client := &S7Client{
		DeviceName: deviceName,
		Client:     s7client,
		Handler:    handler,
	}
	return client

}

func (s *Driver) closeS7Client(deviceName string) {
	s.mu.Lock()
	s7Client := s.s7Clients[deviceName]
	delete(s.s7Clients, deviceName)
	s.mu.Unlock()

	if s7Client != nil && s7Client.Handler != nil {
		s7Client.Handler.Close()
		s7Client.Handler = nil
	}
}

// Get S7Client by 'DeviceName'
func (s *Driver) getS7Client(deviceName string, protocols map[string]models.ProtocolProperties) *S7Client {
	s.mu.Lock()
	s7Client := s.s7Clients[deviceName]
	s.mu.Unlock()

	if s7Client != nil {
		return s7Client
	}

	s.lc.Warnf("S7CLient for device %s not found. Creating it...", deviceName)
	newClient := s.NewS7Client(deviceName, protocols)
	if newClient == nil {
		return nil
	}

	s.mu.Lock()
	existClient := s.s7Clients[deviceName]
	if existClient != nil {
		newClient.Handler.Close()
		s.mu.Unlock()
		return existClient
	}
	s.s7Clients[deviceName] = newClient
	s.mu.Unlock()

	return newClient
}

// transfer DBstring to DBInfo
func (s *Driver) getDBInfo(variable string) (dbInfo *DBInfo, err error) {

	// varibale sample: DB2.DBX1.0 / DB2.DBD26 / DB2.DBD826
	variable = strings.ToUpper(variable)              //upper
	variable = strings.Replace(variable, " ", "", -1) //remove spaces

	if variable == "" {
		s.lc.Errorf("input [NodeName] variable is empty, variable should be S7 syntax")
		return nil, fmt.Errorf("input [NodeName] variable is empty, variable should be S7 syntax")
	}

	var area int
	var amount int
	var wordLen int
	var dbNo int64
	var dbIndex int64
	var dbBit int64
	var dbArray []string

	//var area, dbNumber, start, amount, wordLen int
	switch valueArea := variable[0:2]; valueArea {
	// case "EB": //input byte
	// case "EW": //input word
	// case "ED": //Input double-word
	// case "AB": //Output byte
	// case "AW": //Output word
	// case "AD": //Output double-word
	// case "MB": //Memory byte
	// case "MW": //Memory word
	// case "MD": //Memory double-word
	case "DB": //Data Block
		// Area ID
		// s7areape = 0x81 //process inputs
		// s7areapa = 0x82 //process outputs
		// s7areamk = 0x83 //Merkers
		// s7areadb = 0x84 //DB
		// s7areact = 0x1C //counters
		// s7areatm = 0x1D //timers
		area = 0x84
		amount = 1
		dbArray = strings.Split(variable, ".")
		if len(dbArray) < 2 {
			s.lc.Errorf("Db Area read variable should not be empty")
			return nil, fmt.Errorf("DB variable %+v is invalid", variable)
		}
		dbNo, err = strconv.ParseInt(string(string(dbArray[0])[2:]), 10, 16)
		if err != nil {
			s.lc.Errorf("convert dbNo of %+v to int failed.err:%v", variable, err)
			return nil, fmt.Errorf("convert dbNo of %+v to int failed.err:%v", variable, err)
		}
		dbIndex, err = strconv.ParseInt(string(string(dbArray[1])[3:]), 10, 16)
		if err != nil {
			s.lc.Errorf("convert dbIndex of %+v to int failed.err:%v", variable, err)
			return nil, fmt.Errorf("convert dbIndex of %+v to int failed.err:%v", variable, err)
		}

		dbType := string(dbArray[1])[0:3]

		switch dbType {
		case "DBX": //bit
			wordLen = s7wlbit
			if length := len(dbArray); length != 3 {
				s.lc.Errorf("The point address of %+v is incorrect", variable)
				return nil, fmt.Errorf("the point address of %+v is incorrect", variable)
			}
			// DBIndex = dbIndex + dbBit (DBX12.5 = 12<<3 + 5 = 96+5 = 101 = 0x65)
			dbBit, err := strconv.ParseInt(string(string(dbArray[2])), 10, 16)
			if err != nil {
				s.lc.Errorf("convert dbBit of %+v to int failed.err:%v", variable, err)
				return nil, fmt.Errorf("convert dbBit of %+v to int failed.err:%v", variable, err)
			}
			dbIndex = dbIndex<<3 + dbBit
		case "DBB": //byte
			wordLen = s7wlbyte
		case "DBW": //word
			wordLen = s7wlword
			// amount = 2
		case "DBD": //dword
			wordLen = s7wlreal
			// amount = 4
		default:
			s.lc.Errorf("error when parsing dbtype")
			return nil, fmt.Errorf("error when parsing dbtype")
		}
	default:
		s.lc.Debugf("variable: %+v", variable)
		dbNo = 0
		otherArea := variable[0:1]
		switch otherArea {
		case "I": //input
			area = 0x81
		case "Q": //output
			area = 0x82
		case "M": //memory
			area = 0x83
		case "V": //data block DB1
			dbNo = 1
			area = 0x84
		case "T": //timer
			area = 0x1D
			wordLen = s7wltimer
		case "C": //counter
			area = 0x1C
			wordLen = s7wlcounter
		default:
			s.lc.Errorf("error when parsing db area")
			return nil, fmt.Errorf("error when parsing db area")
		}

		if otherArea == "I" || otherArea == "Q" || otherArea == "M" || otherArea == "V" {
			dbArray = strings.Split(variable, ".")
			if len(dbArray) > 2 {
				s.lc.Errorf("The point address of %+v is incorrect", variable)
				return nil, fmt.Errorf("the point address of %+v is incorrect", variable)
			} else if len(dbArray) == 2 {
				wordLen = s7wlbit
				amount = 1
				// DBIndex = dbIndex + dbBit (I12.5 = 12<<3 + 5 = 96+5 = 101 = 0x65)
				dbBit, err = strconv.ParseInt(string(string(dbArray[1])), 10, 16)
				if err != nil {
					s.lc.Errorf("convert dbBit of %+v to int failed.err:%v", variable, err)
					return nil, fmt.Errorf("convert dbBit of %+v to int failed.err:%v", variable, err)
				}
				dbIndex, err = strconv.ParseInt(string(string(dbArray[0])[1:]), 10, 16)
				if err != nil {
					s.lc.Errorf("convert dbIndex of %+v to int failed.err:%v", variable, err)
					return nil, fmt.Errorf("convert dbIndex of %+v to int failed.err:%v", variable, err)
				}
			} else {
				wordLen = s7wlbyte
				dbType := variable[1:2]
				switch dbType {
				case "B": //byte
					amount = 1
				case "W": //word
					amount = 2
				case "D": //dword
					amount = 4
				case "L": //ulong
					amount = 8
				case "F": //float
					amount = 4
				default:
					s.lc.Errorf("error when parsing dbtype: %+v", dbType)
					return nil, fmt.Errorf("error when parsing dbtype: %+v", dbType)
				}
				dbIndex, err = strconv.ParseInt(variable[2:], 10, 16)
				if err != nil {
					s.lc.Errorf("convert dbIndex of %+v to int failed.err:%v", variable, err)
					return nil, fmt.Errorf("convert dbIndex of %+v to int failed.err:%v", variable, err)
				}
			}
		}
	}

	return &DBInfo{
		Area:       area,
		DBNumber:   int(dbNo),
		Start:      int(dbIndex),
		Bit:        int(dbBit),
		Amount:     amount,
		WordLength: wordLen,
		DBArray:    dbArray,
	}, nil

}

// Get command value type
func getCommandValueType(buffer []byte, valueType string) (value any, err error) {
	var helper gos7.Helper

	switch valueType {
	case common.ValueTypeBool:
		var commandValue bool
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	case common.ValueTypeString:
		var commandValue string
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	case common.ValueTypeUint8:
		var commandValue uint8
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	case common.ValueTypeUint16:
		var commandValue uint16
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	case common.ValueTypeUint32:
		var commandValue uint32
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	case common.ValueTypeUint64:
		var commandValue uint64
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	case common.ValueTypeInt8:
		var commandValue int8
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	case common.ValueTypeInt16:
		var commandValue int16
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	case common.ValueTypeInt32:
		var commandValue int32
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	case common.ValueTypeInt64:
		var commandValue int64
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	case common.ValueTypeFloat32:
		var commandValue float32
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	case common.ValueTypeFloat64:
		var commandValue float64
		helper.GetValueAt(buffer, 0, &commandValue)
		value = commandValue
	default:
		err = fmt.Errorf("fail to convert param, none supported value type: %v", valueType)
	}

	return value, err
}

// Create command value
func newCommandValue(valueType string, param *sdkModel.CommandValue) (any, error) {
	var commandValue any
	var err error
	switch valueType {
	case common.ValueTypeBool:
		commandValue, err = param.BoolValue()
	case common.ValueTypeString:
		commandValue, err = param.StringValue()
	case common.ValueTypeUint8:
		commandValue, err = param.Uint8Value()
	case common.ValueTypeUint16:
		commandValue, err = param.Uint16Value()
	case common.ValueTypeUint32:
		commandValue, err = param.Uint32Value()
	case common.ValueTypeUint64:
		commandValue, err = param.Uint64Value()
	case common.ValueTypeInt8:
		commandValue, err = param.Int8Value()
	case common.ValueTypeInt16:
		commandValue, err = param.Int16Value()
	case common.ValueTypeInt32:
		commandValue, err = param.Int32Value()
	case common.ValueTypeInt64:
		commandValue, err = param.Int64Value()
	case common.ValueTypeFloat32:
		commandValue, err = param.Float32Value()
	case common.ValueTypeFloat64:
		commandValue, err = param.Float64Value()
	default:
		err = fmt.Errorf("fail to convert param, none supported value type: %v", valueType)
	}

	return commandValue, err
}

// Get command value
func getCommandValue(req sdkModel.CommandRequest, reading any) (*sdkModel.CommandValue, error) {
	var err error
	var result = &sdkModel.CommandValue{}
	castError := "fail to parse %v reading, %v"

	if !checkValueInRange(req.Type, reading) {
		err = fmt.Errorf("parse reading fail. Reading %v is out of the value type(%v)'s range", reading, req.Type)
		driver.lc.Error(err.Error())
		return result, err
	}

	var val any
	switch req.Type {
	case common.ValueTypeBool:
		val, err = cast.ToBoolE(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeString:
		val, err = cast.ToStringE(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeUint8:
		val, err = cast.ToUint8E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeUint16:
		val, err = cast.ToUint16E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeUint32:
		val, err = cast.ToUint32E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeUint64:
		val, err = cast.ToUint64E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeInt8:
		val, err = cast.ToInt8E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeInt16:
		val, err = cast.ToInt16E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeInt32:
		val, err = cast.ToInt32E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeInt64:
		val, err = cast.ToInt64E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeFloat32:
		val, err = cast.ToFloat32E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeFloat64:
		val, err = cast.ToFloat64E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
	case common.ValueTypeObject:
		val = reading
	default:
		return nil, fmt.Errorf("return result fail, none supported value type: %v", req.Type)

	}

	result, err = sdkModel.NewCommandValue(req.DeviceResourceName, req.Type, val)
	if err != nil {
		return nil, err
	}
	result.Origin = time.Now().UnixNano()

	return result, nil
}

func (d *Driver) Start() error {

	return nil
}

func (d *Driver) Discover() error {
	return fmt.Errorf("driver's Discover function isn't implemented")
}
