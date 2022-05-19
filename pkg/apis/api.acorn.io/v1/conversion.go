package v1

import (
	"net/url"
	"unsafe"

	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
)

func convert_url_Values_To__ContainerReplicaExecOptions(in *url.Values, out *ContainerReplicaExecOptions, s conversion.Scope) error {
	if values, ok := map[string][]string(*in)["command"]; ok && len(values) > 0 {
		out.Command = *(*[]string)(unsafe.Pointer(&values))
	} else {
		out.Command = nil
	}
	if values, ok := map[string][]string(*in)["tty"]; ok && len(values) > 0 {
		if err := runtime.Convert_Slice_string_To_bool(&values, &out.TTY, s); err != nil {
			return err
		}
	} else {
		out.TTY = false
	}
	if values, ok := map[string][]string(*in)["debugImage"]; ok && len(values) > 0 {
		if err := runtime.Convert_Slice_string_To_string(&values, &out.DebugImage, s); err != nil {
			return err
		}
	} else {
		out.DebugImage = ""
	}
	return nil
}

func Convert_url_Values_To__ContainerReplicaExecOptions(in, out interface{}, s conversion.Scope) error {
	return convert_url_Values_To__ContainerReplicaExecOptions(in.(*url.Values), out.(*ContainerReplicaExecOptions), s)
}
