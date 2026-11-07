/*
 * Cache directive merge for mod_mesi.
 *
 * Lives in its own header (and not in mod_mesi.c) so the C unit tests in
 * test_directives.c exercise the real merge instead of a copy: the merge is
 * pure pointer/sentinel selection and needs no Apache request/server types.
 *
 * NULL means "unset, inherit the parent/global value"; a non-NULL value is
 * explicit and wins even when it is the empty string. That NULL-vs-"" split
 * is what lets a vhost disable an inherited cache or clear an inherited
 * Redis address/password (#617). Do not change the backend check back to
 * `backend[0] != '\0'`: "" is the explicit "disabled" value, not "unset".
 */
#ifndef MOD_MESI_CACHE_CONFIG_H
#define MOD_MESI_CACHE_CONFIG_H

#include <apr_tables.h>

typedef struct {
    /* NULL = unset/inherit; "" = explicitly disabled; otherwise a backend. */
    const char *backend;
    /* >0 = configured, 0 = unset (default 10000). */
    int size;
    /* >=0 = configured, -1 = unset (no expiry). */
    int ttl;
    /* NULL = unset/inherit; "" = explicitly cleared/default localhost:6379. */
    const char *redis_addr;
    /* NULL = unset/inherit; "" = explicitly cleared (no AUTH). */
    const char *redis_password;
    /* >=0 = configured, -1 = unset (libgomesi default 0). */
    int redis_db;
    /* NULL/empty array = unset/inherit; entries = configured list. */
    apr_array_header_t *memcached_servers;
    /* NULL = unset/inherit; "" = DefaultCacheKey (explicit clear). */
    const char *key_template;
} mesi_cache_config;

/* mesi_merge_cache_config merges the cache directives of a parent/global
 * config (base) and a child/vhost config (add). A NULL add field inherits
 * the base field; any non-NULL add field, including "", overrides it. */
static void mesi_merge_cache_config(mesi_cache_config *out,
                                    const mesi_cache_config *base,
                                    const mesi_cache_config *add) {
    out->backend = add->backend ? add->backend : base->backend;
    out->size = (add->size > 0) ? add->size : base->size;
    out->ttl = (add->ttl >= 0) ? add->ttl : base->ttl;
    out->redis_addr = add->redis_addr ? add->redis_addr : base->redis_addr;
    out->redis_password = add->redis_password ? add->redis_password : base->redis_password;
    out->redis_db = (add->redis_db >= 0) ? add->redis_db : base->redis_db;
    out->memcached_servers = (add->memcached_servers && add->memcached_servers->nelts > 0)
                                 ? add->memcached_servers
                                 : base->memcached_servers;
    out->key_template = add->key_template ? add->key_template : base->key_template;
}

#endif
