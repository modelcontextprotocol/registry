# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,089 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 79 distinct user agents.

- **Litware** made 21% of requests and received 35% of the data; 38% of its listing responses were compressed.
- 58 named organizations and products account for 48% of requests and 42% of the data. The rest cannot be tied to a name.
- Across all clients, 34% of listing responses were compressed, carrying 8.6% of listing data.

## How it is used

| Endpoint | Requests | Data sent |
| --- | --- | --- |
| List servers (v0.1) | 67% | 84% |
| List servers (v0) | 12% | 14% |
| Get one server version | 8.4% | 0.6% |
| List one server's versions | 4.4% | 0.3% |
| Other server calls | 4.2% | 0.3% |
| Docs and home page | 2.5% | 0.4% |
| Unknown paths | 0.5% | <0.1% |
| Health and version checks | 0.4% | <0.1% |
| Publish | 0.2% | <0.1% |
| Sign in | 0.2% | <0.1% |

Listing requests, by what the client was doing:

| Listing use | Requests | Data sent |
| --- | --- | --- |
| Paging through all versions | 39% | 60% |
| Paging through latest versions | 44% | 34% |
| First page only | 10% | 5.3% |
| Search by name | 3.7% | 0.9% |
| Incremental sync | 3.4% | 0.2% |

- **Page size:** limit=100 41%, limit=30 17%, limit=50 13%, other 29%.
- **Gateway cache:** 43% of listing requests were served from cache.
- **Responses:** success 96%, not found (404) 1.6%, rate-limited (429) 1.0%, redirect or not modified 0.8%, server error 0.5%, other client error 0.3%.

## Who uses it

By organization or product:

| Organization or product | Identified by | Requests | Data sent | Listings compressed | Addresses seen |
| --- | --- | --- | --- | --- | --- |
| Litware | User agent | 21% | 35% | 38% | 104 |
| proseware-sync | User agent | 1.8% | 2.3% | 0% | 19 |
| relecloud-host | User agent | 9.8% | 0.7% | – | 64 |
| adatum | User agent | 2.2% | 0.5% | 100% | 14 |
| tool-48 | User agent | 0.4% | 0.3% | 0% | 2 |
| tool-42 | User agent | 0.4% | 0.3% | 0% | 2 |
| tool-12 | User agent | 0.4% | 0.3% | 0% | 3 |
| as64511 | User agent | 0.4% | 0.2% | 0% | 2 |
| VanArsdel-Mirror | User agent | 2.6% | 0.2% | 82% | 17 |
| tool-57 | User agent | 0.3% | 0.2% | 0% | 1 |
| tool-29 | User agent | 0.6% | 0.2% | 0% | 3 |
| tool-54 | User agent | 0.3% | 0.2% | 0% | 2 |
| 46 other named |  | 8.0% | 1.6% | 11% | 132 |
| Unnamed clients on internet providers | Not identifiable | 52% | 58% | 33% | 294 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| Litware | 20% | 34% | 39% | 103 | Unknown (100%) | Paging through all versions |
| Go-http-client | 11% | 24% | 0% | 65 | Unknown (100%) | Paging through all versions |
| python-requests | 12% | 19% | 0% | 63 | Unknown (100%) | Paging through latest versions |
| (no user agent) | 4.1% | 5.5% | 0% | 31 | Unknown (100%) | Paging through latest versions |
| node | 12% | 3.3% | 98% | 68 | Unknown (100%) | Paging through latest versions |
| proseware-sync | 1.8% | 2.3% | 0% | 19 | Unknown (100%) | Paging through latest versions |
| litware | 0.6% | 1.5% | 0% | 7 | Unknown (100%) | Paging through all versions |
| Python | 4.0% | 1.3% | 100% | 34 | Unknown (100%) | Paging through latest versions |
| 71 other clients | 34% | 9.8% | 31% | 321 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Unknown | 100% | 100% | 507 |

_Totals are from the gateway's request counters; shares are from 1,733 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._

**Caveats for this period:**

- The network-owner table could not be loaded, so networks show as Unknown.
