# Trove Web Clipper

This Manifest V3 extension captures an article/main content region, a selected passage with basic formatting, or a URL. Chosen PNG/JPEG/GIF images can be retained for offline use. Captures become reviewed drafts in the locally open Trove workspace.

## Install and pair

1. In Trove, open **Capture → Browser clipper → Show bundled extension folder**. Copy that folder to a stable location if you plan to replace the app bundle. The standalone ZIP contains the same unpacked folder.
2. Open `chrome://extensions` (or the equivalent Chromium extensions page), enable **Developer mode**, choose **Load unpacked**, and select that folder. Pin Trove Web Clipper in the browser toolbar.
3. Open its popup and copy the displayed extension ID. The bundled key produces `mmdbnaapeedpfjhafldjcacmanhjmjbb`. In Trove, paste the ID and approve Inbox and/or one selected page, then choose **Create one-time pairing code**.
4. Copy the local endpoint and code into the extension. The code expires after five minutes and works once. Pairing lasts only for the current open app/workspace session; restarting Trove, switching workspace, or revoking requires pairing again. Browser session credentials also expire when the browser session ends.
5. Open an HTTP(S) page, optionally select a passage, then click the toolbar extension. Choose the mode and **Read the active tab**. Review the title and destination. Images are URL references unless you check them and explicitly choose **Acquire selected images once**; approve the requested image origins in the browser permission prompt.
6. Choose **Send draft for review**. In Trove, open **Capture → Browser drafts**, select the draft, review the sanitized text and any omissions, and choose **Save reviewed browser capture**. Inbox means a new unfiled clip. A selected page receives a new child clip; its existing content is preserved.

If the destination changed after capture, Trove rejects the save. **Refresh destination for review** updates its revision; review it again and save separately. Deleted or inaccessible destinations remain blocked. Captures that already reached Trove remain available after restarting, even after pairing is revoked.

## Offline and unavailable app behavior

Reading a page stores one bounded local draft in the browser. If Trove is closed or pairing was revoked, the draft stays local and **Retry retained draft** is available after pairing again. Send or discard that draft before capturing another page. A successful receipt removes the browser copy; the durable Trove draft awaits review. **Forget pairing** removes the browser credential; **Revoke browser pairing** in Trove closes the local listener. Neither action deletes existing drafts or saved clips.

Acquisition is explicit and limited to eight PNG/JPEG/GIF images, 2 MiB total and 20 megapixels per image. Images requiring cookies, authentication, redirects, literal-IP/local-host URLs or other image formats stay as source references. Main-content HTML is limited to 512 KiB; select a smaller passage when necessary. The capture request limit is 4 MiB. A session accepts at most 100 distinct requests before re-pairing. Web clipping does not establish that hidden, paginated or dynamically loaded site content was captured completely.

## Browser and privacy scope

The extension has no background worker, persistent content script or remote JavaScript. `activeTab` permits reading only after the user invokes the toolbar action. The required loopback host permission reaches the explicitly paired endpoint; optional image host permissions are requested per acquisition and removed afterward. Image fetches omit cookies, authentication and referrers and reject redirects. Trove never fetches a URL supplied to the clipper bridge. Original source URL, source bytes, timestamps, extension ID, mode and acquired asset hashes remain in the import provenance; the page adapter strips executable HTML.

The loopback server requires the exact browser extension Origin, exact `127.0.0.1:port` Host, a one-use pairing secret and a session bearer credential. It can only list approved destinations and create review plans. It has no workspace-command, arbitrary-file-write or commit operation. Final saving requires the native owner review path and rechecks destination permissions and revision.

Supported package: Chromium Manifest V3, tested with an isolated Brave Chromium profile on macOS. The native desktop package is macOS. Firefox and Safari extension packages and other desktop platforms are not qualified. This is a developer-mode unpacked install; store publication and its review process are separate. Automated image tests exercise the explicit acquisition action with a permission-grant fixture; interactive browser permission prompts need installation smoke testing.
