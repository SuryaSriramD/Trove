//go:build darwin && cgo
#import <Foundation/Foundation.h>
#import <ServiceManagement/ServiceManagement.h>
#include <stdlib.h>
#include <string.h>

char *trove_reminder_service(int action) {
 @autoreleasepool {
  SMAppService *service = [SMAppService loginItemServiceWithIdentifier:@"local.trove.desktop.reminders"];
  NSError *error = nil;
  if (action == 1 && service.status != SMAppServiceStatusEnabled) [service registerAndReturnError:&error];
  if (action == 2 && service.status != SMAppServiceStatusNotRegistered && service.status != SMAppServiceStatusNotFound) [service unregisterAndReturnError:&error];
  NSString *status = @"unknown";
  switch(service.status) {
   case SMAppServiceStatusNotRegistered: status=@"notRegistered"; break;
   case SMAppServiceStatusEnabled: status=@"enabled"; break;
   case SMAppServiceStatusRequiresApproval: status=@"requiresApproval"; break;
   case SMAppServiceStatusNotFound: status=@"notFound"; break;
  }
  NSDictionary *reply=@{@"status":status,@"error":error ? @"The OS could not change reminder registration. Check Login Items in System Settings and the signed application installation." : @""};
  NSData *data=[NSJSONSerialization dataWithJSONObject:reply options:0 error:nil];
  if (!data) return NULL;
  char *out=malloc(data.length+1); if(!out) return NULL; memcpy(out,data.bytes,data.length);out[data.length]=0;return out;
 }
}
