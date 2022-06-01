// The MIT License
//
// Copyright (c) 2020 Temporal Technologies Inc.  All rights reserved.
//
// Copyright (c) 2020 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

// Code generated by MockGen. DO NOT EDIT.
// Source: ack_manager.go

// Package replication is a generated GoMock package.
package replication

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	repication "go.temporal.io/server/api/replication/v1"
	tasks "go.temporal.io/server/service/history/tasks"
)

// MockAckManager is a mock of AckManager interface.
type MockAckManager struct {
	ctrl     *gomock.Controller
	recorder *MockAckManagerMockRecorder
}

// MockAckManagerMockRecorder is the mock recorder for MockAckManager.
type MockAckManagerMockRecorder struct {
	mock *MockAckManager
}

// NewMockAckManager creates a new mock instance.
func NewMockAckManager(ctrl *gomock.Controller) *MockAckManager {
	mock := &MockAckManager{ctrl: ctrl}
	mock.recorder = &MockAckManagerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAckManager) EXPECT() *MockAckManagerMockRecorder {
	return m.recorder
}

// GetMaxTaskID mocks base method.
func (m *MockAckManager) GetMaxTaskID() int64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMaxTaskID")
	ret0, _ := ret[0].(int64)
	return ret0
}

// GetMaxTaskID indicates an expected call of GetMaxTaskID.
func (mr *MockAckManagerMockRecorder) GetMaxTaskID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMaxTaskID", reflect.TypeOf((*MockAckManager)(nil).GetMaxTaskID))
}

// GetTask mocks base method.
func (m *MockAckManager) GetTask(ctx context.Context, taskInfo *repication.ReplicationTaskInfo) (*repication.ReplicationTask, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetTask", ctx, taskInfo)
	ret0, _ := ret[0].(*repication.ReplicationTask)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetTask indicates an expected call of GetTask.
func (mr *MockAckManagerMockRecorder) GetTask(ctx, taskInfo interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetTask", reflect.TypeOf((*MockAckManager)(nil).GetTask), ctx, taskInfo)
}

// GetTasks mocks base method.
func (m *MockAckManager) GetTasks(ctx context.Context, pollingCluster string, queryMessageID int64) (*repication.ReplicationMessages, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetTasks", ctx, pollingCluster, queryMessageID)
	ret0, _ := ret[0].(*repication.ReplicationMessages)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetTasks indicates an expected call of GetTasks.
func (mr *MockAckManagerMockRecorder) GetTasks(ctx, pollingCluster, queryMessageID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetTasks", reflect.TypeOf((*MockAckManager)(nil).GetTasks), ctx, pollingCluster, queryMessageID)
}

// NotifyNewTasks mocks base method.
func (m *MockAckManager) NotifyNewTasks(tasks []tasks.Task) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "NotifyNewTasks", tasks)
}

// NotifyNewTasks indicates an expected call of NotifyNewTasks.
func (mr *MockAckManagerMockRecorder) NotifyNewTasks(tasks interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NotifyNewTasks", reflect.TypeOf((*MockAckManager)(nil).NotifyNewTasks), tasks)
}
