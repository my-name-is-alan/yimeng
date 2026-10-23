//go:build windows

package vault

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows mode bits cannot describe a DACL. Restrict the key to the process
// user and LocalSystem, with inheritance disabled, including at file creation.
func keySecurityDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := user.User.Sid.String()
	sddl := "O:" + sid + "D:P(A;;FA;;;" + sid + ")"
	if sid != "S-1-5-18" {
		sddl += "(A;;FA;;;SY)"
	}
	return windows.SecurityDescriptorFromString(sddl)
}

func createKeyFile(path string) (*os.File, error) {
	sd, err := keySecurityDescriptor()
	if err != nil {
		return nil, err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "create", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

func keyACLMatches(got, want *windows.SECURITY_DESCRIPTOR) bool {
	owner, _, err := got.Owner()
	if err != nil || owner == nil {
		return false
	}
	wantOwner, _, err := want.Owner()
	if err != nil || wantOwner == nil || !owner.Equals(wantOwner) {
		return false
	}
	control, _, err := got.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return false
	}
	acl, _, err := got.DACL()
	if err != nil || acl == nil {
		return false
	}
	wantACL, _, err := want.DACL()
	if err != nil || wantACL == nil || acl.AceCount != wantACL.AceCount {
		return false
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var a, b *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &a) != nil || windows.GetAce(wantACL, i, &b) != nil {
			return false
		}
		if a.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || a.Header.AceFlags != b.Header.AceFlags || a.Mask != b.Mask {
			return false
		}
		if !(*windows.SID)(unsafe.Pointer(&a.SidStart)).Equals((*windows.SID)(unsafe.Pointer(&b.SidStart))) {
			return false
		}
	}
	return true
}

func secureKeyFile(path string) (string, error) {
	want, err := keySecurityDescriptor()
	if err != nil {
		return "", err
	}
	info := windows.SECURITY_INFORMATION(windows.OWNER_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION)
	got, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, info)
	if err != nil {
		return "", err
	}
	if keyACLMatches(got, want) {
		return "", nil
	}
	owner, _, err := want.Owner()
	if err != nil {
		return "", err
	}
	acl, _, err := want.DACL()
	if err != nil {
		return "", err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, info|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, acl, nil); err != nil {
		return "", err
	}
	got, err = windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, info)
	if err != nil {
		return "", err
	}
	if !keyACLMatches(got, want) {
		return "", fmt.Errorf("master key ACL could not be restricted")
	}
	return fmt.Sprintf("master key %s ACL restricted to current user and LocalSystem", path), nil
}
