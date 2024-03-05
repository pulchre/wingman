// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.31.0
// 	protoc        v4.25.3
// source: processor.proto

package grpc

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type Type int32

const (
	Type_CONNECT  Type = 0
	Type_JOB      Type = 1
	Type_RESULT   Type = 2
	Type_SHUTDOWN Type = 3
)

// Enum value maps for Type.
var (
	Type_name = map[int32]string{
		0: "CONNECT",
		1: "JOB",
		2: "RESULT",
		3: "SHUTDOWN",
	}
	Type_value = map[string]int32{
		"CONNECT":  0,
		"JOB":      1,
		"RESULT":   2,
		"SHUTDOWN": 3,
	}
)

func (x Type) Enum() *Type {
	p := new(Type)
	*p = x
	return p
}

func (x Type) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (Type) Descriptor() protoreflect.EnumDescriptor {
	return file_processor_proto_enumTypes[0].Descriptor()
}

func (Type) Type() protoreflect.EnumType {
	return &file_processor_proto_enumTypes[0]
}

func (x Type) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use Type.Descriptor instead.
func (Type) EnumDescriptor() ([]byte, []int) {
	return file_processor_proto_rawDescGZIP(), []int{0}
}

type Message struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Type  Type   `protobuf:"varint,1,opt,name=Type,proto3,enum=wingman.Type" json:"Type,omitempty"`
	Job   *Job   `protobuf:"bytes,2,opt,name=Job,proto3" json:"Job,omitempty"`
	PID   int32  `protobuf:"varint,3,opt,name=PID,proto3" json:"PID,omitempty"`
	Error *Error `protobuf:"bytes,4,opt,name=Error,proto3" json:"Error,omitempty"`
}

func (x *Message) Reset() {
	*x = Message{}
	if protoimpl.UnsafeEnabled {
		mi := &file_processor_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Message) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Message) ProtoMessage() {}

func (x *Message) ProtoReflect() protoreflect.Message {
	mi := &file_processor_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Message.ProtoReflect.Descriptor instead.
func (*Message) Descriptor() ([]byte, []int) {
	return file_processor_proto_rawDescGZIP(), []int{0}
}

func (x *Message) GetType() Type {
	if x != nil {
		return x.Type
	}
	return Type_CONNECT
}

func (x *Message) GetJob() *Job {
	if x != nil {
		return x.Job
	}
	return nil
}

func (x *Message) GetPID() int32 {
	if x != nil {
		return x.PID
	}
	return 0
}

func (x *Message) GetError() *Error {
	if x != nil {
		return x.Error
	}
	return nil
}

type Job struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	ID       string `protobuf:"bytes,1,opt,name=ID,proto3" json:"ID,omitempty"`
	LockID   int32  `protobuf:"varint,2,opt,name=LockID,proto3" json:"LockID,omitempty"`
	TypeName string `protobuf:"bytes,3,opt,name=TypeName,proto3" json:"TypeName,omitempty"`
	Payload  []byte `protobuf:"bytes,4,opt,name=Payload,proto3" json:"Payload,omitempty"`
}

func (x *Job) Reset() {
	*x = Job{}
	if protoimpl.UnsafeEnabled {
		mi := &file_processor_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Job) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Job) ProtoMessage() {}

func (x *Job) ProtoReflect() protoreflect.Message {
	mi := &file_processor_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Job.ProtoReflect.Descriptor instead.
func (*Job) Descriptor() ([]byte, []int) {
	return file_processor_proto_rawDescGZIP(), []int{1}
}

func (x *Job) GetID() string {
	if x != nil {
		return x.ID
	}
	return ""
}

func (x *Job) GetLockID() int32 {
	if x != nil {
		return x.LockID
	}
	return 0
}

func (x *Job) GetTypeName() string {
	if x != nil {
		return x.TypeName
	}
	return ""
}

func (x *Job) GetPayload() []byte {
	if x != nil {
		return x.Payload
	}
	return nil
}

type Error struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Message string `protobuf:"bytes,1,opt,name=message,proto3" json:"message,omitempty"`
}

func (x *Error) Reset() {
	*x = Error{}
	if protoimpl.UnsafeEnabled {
		mi := &file_processor_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Error) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Error) ProtoMessage() {}

func (x *Error) ProtoReflect() protoreflect.Message {
	mi := &file_processor_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Error.ProtoReflect.Descriptor instead.
func (*Error) Descriptor() ([]byte, []int) {
	return file_processor_proto_rawDescGZIP(), []int{2}
}

func (x *Error) GetMessage() string {
	if x != nil {
		return x.Message
	}
	return ""
}

var File_processor_proto protoreflect.FileDescriptor

var file_processor_proto_rawDesc = []byte{
	0x0a, 0x0f, 0x70, 0x72, 0x6f, 0x63, 0x65, 0x73, 0x73, 0x6f, 0x72, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x12, 0x07, 0x77, 0x69, 0x6e, 0x67, 0x6d, 0x61, 0x6e, 0x22, 0x84, 0x01, 0x0a, 0x07, 0x4d,
	0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x12, 0x21, 0x0a, 0x04, 0x54, 0x79, 0x70, 0x65, 0x18, 0x01,
	0x20, 0x01, 0x28, 0x0e, 0x32, 0x0d, 0x2e, 0x77, 0x69, 0x6e, 0x67, 0x6d, 0x61, 0x6e, 0x2e, 0x54,
	0x79, 0x70, 0x65, 0x52, 0x04, 0x54, 0x79, 0x70, 0x65, 0x12, 0x1e, 0x0a, 0x03, 0x4a, 0x6f, 0x62,
	0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x0c, 0x2e, 0x77, 0x69, 0x6e, 0x67, 0x6d, 0x61, 0x6e,
	0x2e, 0x4a, 0x6f, 0x62, 0x52, 0x03, 0x4a, 0x6f, 0x62, 0x12, 0x10, 0x0a, 0x03, 0x50, 0x49, 0x44,
	0x18, 0x03, 0x20, 0x01, 0x28, 0x05, 0x52, 0x03, 0x50, 0x49, 0x44, 0x12, 0x24, 0x0a, 0x05, 0x45,
	0x72, 0x72, 0x6f, 0x72, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x0e, 0x2e, 0x77, 0x69, 0x6e,
	0x67, 0x6d, 0x61, 0x6e, 0x2e, 0x45, 0x72, 0x72, 0x6f, 0x72, 0x52, 0x05, 0x45, 0x72, 0x72, 0x6f,
	0x72, 0x22, 0x63, 0x0a, 0x03, 0x4a, 0x6f, 0x62, 0x12, 0x0e, 0x0a, 0x02, 0x49, 0x44, 0x18, 0x01,
	0x20, 0x01, 0x28, 0x09, 0x52, 0x02, 0x49, 0x44, 0x12, 0x16, 0x0a, 0x06, 0x4c, 0x6f, 0x63, 0x6b,
	0x49, 0x44, 0x18, 0x02, 0x20, 0x01, 0x28, 0x05, 0x52, 0x06, 0x4c, 0x6f, 0x63, 0x6b, 0x49, 0x44,
	0x12, 0x1a, 0x0a, 0x08, 0x54, 0x79, 0x70, 0x65, 0x4e, 0x61, 0x6d, 0x65, 0x18, 0x03, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x08, 0x54, 0x79, 0x70, 0x65, 0x4e, 0x61, 0x6d, 0x65, 0x12, 0x18, 0x0a, 0x07,
	0x50, 0x61, 0x79, 0x6c, 0x6f, 0x61, 0x64, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x07, 0x50,
	0x61, 0x79, 0x6c, 0x6f, 0x61, 0x64, 0x22, 0x21, 0x0a, 0x05, 0x45, 0x72, 0x72, 0x6f, 0x72, 0x12,
	0x18, 0x0a, 0x07, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09,
	0x52, 0x07, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x2a, 0x36, 0x0a, 0x04, 0x54, 0x79, 0x70,
	0x65, 0x12, 0x0b, 0x0a, 0x07, 0x43, 0x4f, 0x4e, 0x4e, 0x45, 0x43, 0x54, 0x10, 0x00, 0x12, 0x07,
	0x0a, 0x03, 0x4a, 0x4f, 0x42, 0x10, 0x01, 0x12, 0x0a, 0x0a, 0x06, 0x52, 0x45, 0x53, 0x55, 0x4c,
	0x54, 0x10, 0x02, 0x12, 0x0c, 0x0a, 0x08, 0x53, 0x48, 0x55, 0x54, 0x44, 0x4f, 0x57, 0x4e, 0x10,
	0x03, 0x32, 0x43, 0x0a, 0x09, 0x50, 0x72, 0x6f, 0x63, 0x65, 0x73, 0x73, 0x6f, 0x72, 0x12, 0x36,
	0x0a, 0x0a, 0x49, 0x6e, 0x69, 0x74, 0x69, 0x61, 0x6c, 0x69, 0x7a, 0x65, 0x12, 0x10, 0x2e, 0x77,
	0x69, 0x6e, 0x67, 0x6d, 0x61, 0x6e, 0x2e, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x1a, 0x10,
	0x2e, 0x77, 0x69, 0x6e, 0x67, 0x6d, 0x61, 0x6e, 0x2e, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65,
	0x22, 0x00, 0x28, 0x01, 0x30, 0x01, 0x42, 0x21, 0x5a, 0x1f, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62,
	0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x70, 0x75, 0x6c, 0x63, 0x68, 0x72, 0x65, 0x2f, 0x77, 0x69, 0x6e,
	0x67, 0x6d, 0x61, 0x6e, 0x3b, 0x67, 0x72, 0x70, 0x63, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x33,
}

var (
	file_processor_proto_rawDescOnce sync.Once
	file_processor_proto_rawDescData = file_processor_proto_rawDesc
)

func file_processor_proto_rawDescGZIP() []byte {
	file_processor_proto_rawDescOnce.Do(func() {
		file_processor_proto_rawDescData = protoimpl.X.CompressGZIP(file_processor_proto_rawDescData)
	})
	return file_processor_proto_rawDescData
}

var file_processor_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_processor_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_processor_proto_goTypes = []interface{}{
	(Type)(0),       // 0: wingman.Type
	(*Message)(nil), // 1: wingman.Message
	(*Job)(nil),     // 2: wingman.Job
	(*Error)(nil),   // 3: wingman.Error
}
var file_processor_proto_depIdxs = []int32{
	0, // 0: wingman.Message.Type:type_name -> wingman.Type
	2, // 1: wingman.Message.Job:type_name -> wingman.Job
	3, // 2: wingman.Message.Error:type_name -> wingman.Error
	1, // 3: wingman.Processor.Initialize:input_type -> wingman.Message
	1, // 4: wingman.Processor.Initialize:output_type -> wingman.Message
	4, // [4:5] is the sub-list for method output_type
	3, // [3:4] is the sub-list for method input_type
	3, // [3:3] is the sub-list for extension type_name
	3, // [3:3] is the sub-list for extension extendee
	0, // [0:3] is the sub-list for field type_name
}

func init() { file_processor_proto_init() }
func file_processor_proto_init() {
	if File_processor_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_processor_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Message); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_processor_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Job); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_processor_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Error); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_processor_proto_rawDesc,
			NumEnums:      1,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_processor_proto_goTypes,
		DependencyIndexes: file_processor_proto_depIdxs,
		EnumInfos:         file_processor_proto_enumTypes,
		MessageInfos:      file_processor_proto_msgTypes,
	}.Build()
	File_processor_proto = out.File
	file_processor_proto_rawDesc = nil
	file_processor_proto_goTypes = nil
	file_processor_proto_depIdxs = nil
}
