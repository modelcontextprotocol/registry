# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,073 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 73 distinct user agents.

- **Litware** made 19% of requests and received 31% of the data; 38% of its listing responses were compressed.
- 55 named organizations and products account for 41% of requests and 39% of the data. The rest cannot be tied to a name.
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
| Litware | User agent and network owner | 19% | 31% | 38% | 99 |
| Acme Widgets Inc | Network owner | 2.3% | 3.1% | 0% | 24 |
| proseware-sync | User agent | 1.5% | 1.6% | 0% | 10 |
| Tailspin Toys | Network owner | 0.6% | 0.4% | 0% | 6 |
| relecloud-host | User agent | 5.7% | 0.4% | – | 62 |
| Lakeshore Dispatch | Network owner | 0.5% | 0.3% | 0% | 3 |
| tool-48 | User agent | 0.4% | 0.3% | 0% | 2 |
| adatum | User agent | 1.1% | 0.3% | 100% | 15 |
| tool-39 | User agent | 0.4% | 0.3% | 0% | 3 |
| tool-24 | User agent | 0.2% | 0.2% | 0% | 1 |
| trey-agent | User agent | 0.6% | 0.2% | 0% | 3 |
| VanArsdel-Mirror | User agent | 2.6% | 0.1% | 80% | 15 |
| 43 other named |  | 6.2% | 0.6% | 32% | 168 |
| Unnamed clients on cloud hosting | Not identifiable | 46% | 57% | 33% | 172 |
| Unnamed clients on internet providers | Not identifiable | 13% | 4.2% | 11% | 130 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| (not recorded) | 36% | 34% | 35% | 359 | Mixed | Paging through latest versions |
| Litware | 12% | 20% | 39% | 72 | Litware (80%) | Paging through all versions |
| python-requests | 9.4% | 16% | 0% | 61 | Amazon (AWS) (81%) | Paging through latest versions |
| Go-http-client | 6.1% | 13% | 0% | 44 | Google (79%) | Paging through all versions |
| litware | 1.1% | 2.7% | 0% | 7 | Litware (100%) | Paging through all versions |
| (no user agent) | 2.0% | 2.6% | 0% | 18 | Amazon (AWS) (82%) | Paging through latest versions |
| node | 7.2% | 2.1% | 96% | 57 | Microsoft (Azure) (81%) | Paging through latest versions |
| proseware-sync | 1.5% | 1.6% | 0% | 10 | Microsoft (Azure) (100%) | Paging through latest versions |
| 65 other clients | 25% | 8.0% | 37% | 316 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Amazon (AWS) | 27% | 34% | 90 |
| Litware | 16% | 26% | 82 |
| Google | 11% | 23% | 32 |
| Microsoft (Azure) | 13% | 5.7% | 32 |
| Acme Widgets Inc | 3.7% | 3.8% | 32 |
| Example Telecom Ltd | 5.9% | 1.9% | 32 |
| 39 other networks | 23% | 6.1% | 222 |

_Totals are from the gateway's request counters; shares are from 1,760 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._

**Caveats for this period:**

- Some records predate user-agent logging (Nov 12 09:10 UTC) and show as "(not recorded)".
