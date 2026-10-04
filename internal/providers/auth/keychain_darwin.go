//go:build darwin && cgo

package auth

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>
static CFMutableDictionaryRef trove_query(const char *service, const char *account) {
 CFMutableDictionaryRef q=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFStringRef s=CFStringCreateWithCString(NULL,service,kCFStringEncodingUTF8), a=CFStringCreateWithCString(NULL,account,kCFStringEncodingUTF8);
 CFDictionarySetValue(q,kSecClass,kSecClassGenericPassword);
 CFDictionarySetValue(q,kSecAttrService,s); CFDictionarySetValue(q,kSecAttrAccount,a);
 CFDictionarySetValue(q,kSecAttrSynchronizable,kCFBooleanFalse);
 CFRelease(s); CFRelease(a); return q;
}
static OSStatus trove_get(const char *service,const char *account,void **bytes,long *length) {
 CFMutableDictionaryRef q=trove_query(service,account);
 CFDictionarySetValue(q,kSecReturnData,kCFBooleanTrue); CFDictionarySetValue(q,kSecMatchLimit,kSecMatchLimitOne);
 CFTypeRef result=NULL; OSStatus status=SecItemCopyMatching(q,&result); CFRelease(q);
 if(status!=errSecSuccess) return status;
 if(CFGetTypeID(result)!=CFDataGetTypeID()) { CFRelease(result); return errSecDecode; }
 *length=CFDataGetLength((CFDataRef)result);
 if(*length>131072) { CFRelease(result); return errSecDecode; }
 *bytes=malloc(*length); if(*length>0 && !*bytes) { CFRelease(result); return errSecAllocate; }
 memcpy(*bytes,CFDataGetBytePtr((CFDataRef)result),*length); CFRelease(result); return errSecSuccess;
}
static OSStatus trove_put(const char *service,const char *account,const void *bytes,long length) {
 CFMutableDictionaryRef q=trove_query(service,account), values=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFDataRef data=CFDataCreate(NULL,bytes,length);
 CFDictionarySetValue(values,kSecValueData,data);
 OSStatus status=SecItemUpdate(q,values);
 if(status==errSecItemNotFound) {
  CFDictionarySetValue(q,kSecValueData,data);
  CFDictionarySetValue(q,kSecAttrAccessible,kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly);
  status=SecItemAdd(q,NULL);
 }
 CFRelease(data); CFRelease(values); CFRelease(q); return status;
}
static OSStatus trove_delete(const char *service,const char *account) {
 CFMutableDictionaryRef q=trove_query(service,account); OSStatus status=SecItemDelete(q); CFRelease(q); return status;
}
*/
import "C"
import (
	"context"
	"crypto/sha256"
	"fmt"
	"unsafe"
)

type keychainStore struct{ service string }

func nativeSecretStore(root string) (SecretStore, error) {
	sum := sha256.Sum256([]byte(root))
	return &keychainStore{service: fmt.Sprintf("org.trove.siwc.%x", sum[:16])}, nil
}
func (s *keychainStore) Get(ctx context.Context, id string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service, account := C.CString(s.service), C.CString(id)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))
	var data unsafe.Pointer
	var length C.long
	status := C.trove_get(service, account, &data, &length)
	if status == C.errSecItemNotFound {
		return nil, ErrSecretMissing
	}
	if status != C.errSecSuccess {
		return nil, fail("CREDENTIAL_STORE_FAILED", "The operating system credential store could not read this account")
	}
	defer C.free(data)
	return C.GoBytes(data, C.int(length)), nil
}
func (s *keychainStore) Put(ctx context.Context, id string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 131072 {
		return fail("CREDENTIAL_STORE_FAILED", "Credential record is invalid")
	}
	service, account := C.CString(s.service), C.CString(id)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))
	if C.trove_put(service, account, unsafe.Pointer(&data[0]), C.long(len(data))) != C.errSecSuccess {
		return fail("CREDENTIAL_STORE_FAILED", "The operating system credential store could not save this account")
	}
	return nil
}
func (s *keychainStore) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	service, account := C.CString(s.service), C.CString(id)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))
	status := C.trove_delete(service, account)
	if status != C.errSecSuccess && status != C.errSecItemNotFound {
		return fail("CREDENTIAL_STORE_FAILED", "The operating system credential store could not clear this account")
	}
	return nil
}
