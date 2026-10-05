// Independent reproduction of the public system dependency used by JSC's
// Intl.Collator. No vendor process, credentials, network, or user files.
#include <stdint.h>
#include <stdio.h>
extern void *ucol_open(const char *, int32_t *);
extern void ucol_close(void *);
extern const char *u_errorName(int32_t);
int main(void) {
    int32_t status = 0;
    void *collator = ucol_open("en_US", &status);
    printf("{\"status\":%d,\"name\":\"%s\",\"collator\":%s}\n", status,
           u_errorName(status), collator ? "true" : "false");
    if (collator) ucol_close(collator);
    return 0;
}
