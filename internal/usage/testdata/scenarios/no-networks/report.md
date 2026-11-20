# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,073 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 84 distinct user agents.

- **Litware** made 20% of requests and received 33% of the data; 40% of its listing responses were compressed.
- 58 named organizations and products account for 46% of requests and 40% of the data. The rest cannot be tied to a name.
- Across all clients, 32% of listing responses were compressed, carrying 8.2% of listing data.

## How it is used

| Endpoint | Requests | Data sent |
| --- | --- | --- |
| List servers (v0.1) | 67% | 84% |
| List servers (v0) | 12% | 14% |
| Get one server version | 8.1% | 0.6% |
| List one server's versions | 4.9% | 0.3% |
| Other server calls | 4.4% | 0.3% |
| Docs and home page | 2.3% | 0.5% |
| Unknown paths | 0.5% | <0.1% |
| Health and version checks | 0.4% | <0.1% |
| Sign in | 0.2% | <0.1% |
| Publish | 0.2% | <0.1% |

Listing requests, by what the client was doing:

| Listing use | Requests | Data sent |
| --- | --- | --- |
| Paging through all versions | 36% | 57% |
| Paging through latest versions | 44% | 35% |
| First page only | 13% | 7.6% |
| Search by name | 2.9% | 0.5% |
| Incremental sync | 4.5% | 0.2% |

- **Page size:** limit=100 40%, limit=30 16%, limit=50 12%, other 31%.
- **Gateway cache:** 41% of listing requests were served from cache.
- **Responses:** success 95%, redirect or not modified 1.9%, not found (404) 1.8%, rate-limited (429) 1.0%, server error 0.4%, other client error 0.3%.

## Who uses it

By organization or product:

| Organization or product | Identified by | Requests | Data sent | Listings compressed | Addresses seen |
| --- | --- | --- | --- | --- | --- |
| Litware | User agent | 20% | 33% | 40% | 106 |
| proseware-sync | User agent | 2.4% | 2.9% | 0% | 14 |
| relecloud-host | User agent | 9.3% | 0.7% | – | 63 |
| adatum | User agent | 2.1% | 0.6% | 99.9% | 22 |
| tool-48 | User agent | 0.4% | 0.3% | 0% | 2 |
| tool-39 | User agent | 0.4% | 0.3% | 0% | 3 |
| tool-30 | User agent | 0.3% | 0.2% | 0% | 1 |
| trey-agent | User agent | 0.8% | 0.2% | 0% | 6 |
| tool-57 | User agent | 0.3% | 0.2% | 0% | 1 |
| tool-24 | User agent | 0.2% | 0.2% | 0% | 1 |
| VanArsdel-Mirror | User agent | 3.5% | 0.2% | 84% | 18 |
| ExampleBot | User agent | 0.8% | 0.2% | 100% | 20 |
| 46 other named |  | 5.4% | 0.6% | 0% | 130 |
| Unnamed clients on internet providers | Not identifiable | 54% | 60% | 28% | 317 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| Litware | 19% | 31% | 42% | 103 | Unknown (100%) | Paging through all versions |
| Go-http-client | 10% | 23% | 0% | 58 | Unknown (100%) | Paging through all versions |
| python-requests | 13% | 22% | 0% | 78 | Unknown (100%) | Paging through latest versions |
| (no user agent) | 2.9% | 3.6% | 0% | 33 | Unknown (100%) | Paging through latest versions |
| node | 11% | 3.3% | 95% | 67 | Unknown (100%) | Paging through latest versions |
| proseware-sync | 2.4% | 2.9% | 0% | 14 | Unknown (100%) | Paging through latest versions |
| litware | 1.1% | 2.7% | 0% | 8 | Unknown (100%) | Paging through all versions |
| Python | 3.4% | 1.1% | 100% | 30 | Unknown (100%) | Paging through latest versions |
| 76 other clients | 36% | 11% | 26% | 377 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Unknown | 100% | 100% | 522 |

_Totals are from the gateway's request counters; shares are from 1,760 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._

**Caveats for this period:**

- The network-owner table could not be loaded, so networks show as Unknown.
