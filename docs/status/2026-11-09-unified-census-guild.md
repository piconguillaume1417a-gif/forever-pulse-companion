# Local rc.11 candidate: streamed SavedVariables and common guild upload

The ordinary file watcher and --once consume the file with an incremental
data parser. The scanner validates syntax and hashes once, then reads bounded
record sections from the same shared file handle. It checks identity, size and
mtime again immediately before the disk transaction commits. An invalid tail
never queues a valid prefix. Lua is never executed. The old --dump comparison
tool and third-party Auctionator parser retain their separate whole-value
limit. The Auctionator notes reader now extracts only its small fields from
the Forever Pulse save.

The file's compact shared name dictionary is read by block; only a bounded
cache of blocks is kept decoded. The validation of a reconstructed roster
uses its original complete/delta chain. Statistics are staged in groups
bounded by both bytes and count. The existing queue and receipt handling are
used; the cumul loads just matching identities and separates regional scopes
where the public realm/mode/faction key collides. No new total-file limit.
Metadata offsets scale with the number of records. SQLite needs disk space
for the queue and transaction.

A complete legacy or new-addon guild snapshot is validated before partitioning.
Guild parts (250 max) are additive observations with canonical, stable IDs
and explicit tri-state online data. False and null do not change the last
presence time. The old queue still drains through the rc.10 compatibility
route; new saves do not populate it.

Local Windows evidence: test suite/vet; multi-block dictionary with
out-of-order delta; two regions with the same GUID and public scope; malformed
tail and .bak; 501 members/3 requests, interrupted after a durable ACK and
restarted; max JSON request 24,543 bytes. 83,912,630-byte fixture: 323 ms,
12,127,840 sampled Go heap bytes, 160 Statistics sheets. 402,775,190-byte
fixture: 1.296 s, 21,700,120 sampled Go heap bytes, 768 sheets. Both had one
Census batch and a large non-transmitted index. Synthetic and local only.

Windows CLI and PE product version: 0.10.0-rc.11. SHA-256 of local executable:
58e3b741ae14306d83265df430f50cb5d67f78a5dfa8a99ae1bf5981af1bbd52.
The executable was built and checked, not installed. No MSI, hosted CI, tag
or release. A site with the guild Census handler must precede its use with
guild data; update the Companion before the addon.
