// Pre-dispatch network/resource policy in WebKit's web process, not the UI process.
#include "document-policy.hpp"
#include <webkit2/webkit-web-extension.h>
#include <cstring>
namespace td=trove_document;
namespace {
td::Resources resources;
bool valid=false,render=false;
constexpr const char *Origin="trove-document://document/";
gboolean request(WebKitWebPage*,WebKitURIRequest *input,WebKitURIResponse*,gpointer) {
 try {
  td::require(valid); const char *raw=webkit_uri_request_get_uri(input);td::require(raw);
  std::string uri(raw);td::require(uri.rfind(Origin,0)==0);
  // UI scheme handler additionally requires completed print + frozen hash for result.pdf.
  td::request_path(std::string(td::Origin)+uri.substr(std::strlen(Origin)),webkit_uri_request_get_http_method(input),resources,render);
  return FALSE;
 } catch(...) { return TRUE; }
}
void page_created(WebKitWebExtension*,WebKitWebPage *page,gpointer) {g_signal_connect(page,"send-request",G_CALLBACK(request),nullptr);if(valid)webkit_web_page_send_message_to_view(page,webkit_user_message_new("trove-document-policy-ready",nullptr),nullptr,nullptr,nullptr);}
}
extern "C" G_MODULE_EXPORT void webkit_web_extension_initialize_with_user_data(WebKitWebExtension *extension,const GVariant *data) {
 // Install deny-by-default even if initialization data is absent or invalid.
 g_signal_connect(extension,"page-created",G_CALLBACK(page_created),nullptr);
 try {
  auto *value=const_cast<GVariant*>(data);
  td::require(value&&g_variant_is_of_type(value,G_VARIANT_TYPE_STRING));
  const char *raw=g_variant_get_string(value,nullptr);td::require(raw);
  auto parsed=td::parse(raw,2*td::MaxMap);auto mode=parsed.at("mode").get<std::string>();
  td::require(mode=="render"||mode=="extract");resources=td::resources(parsed.at("resources").get<std::string>(),mode);render=mode=="render";valid=true;
 } catch(...) {valid=false;}
}
