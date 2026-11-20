# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,089 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 72 distinct user agents.

- **Litware** made 19% of requests and received 32% of the data; 40% of its listing responses were compressed.
- 57 named organizations and products account for 40% of requests and 37% of the data. The rest cannot be tied to a name.
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
| Litware | User agent and network owner | 19% | 32% | 40% | 98 |
| proseware-sync | User agent | 1.1% | 1.4% | 0% | 14 |
| Tailspin Toys | Network owner | 0.8% | 0.6% | 0% | 6 |
| Acme Widgets Inc | Network owner | 1.1% | 0.6% | 0% | 16 |
| relecloud-host | User agent | 5.9% | 0.4% | – | 62 |
| adatum | User agent | 1.5% | 0.4% | 100% | 8 |
| VanArsdel-Mirror | User agent | 2.2% | 0.2% | 84% | 9 |
| AS64511 | User agent and network owner | 0.5% | 0.2% | 0% | 12 |
| tool-48 | User agent | 0.2% | 0.1% | 0% | 1 |
| tool-45 | User agent | 0.2% | 0.1% | 0% | 1 |
| tool-42 | User agent | 0.2% | 0.1% | 0% | 1 |
| tool-39 | User agent | 0.2% | 0.1% | 0% | 2 |
| 45 other named |  | 7.4% | 1.1% | 11% | 150 |
| Unnamed clients on cloud hosting | Not identifiable | 48% | 60% | 34% | 166 |
| Unnamed clients on internet providers | Not identifiable | 12% | 3.3% | 17% | 132 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| (not recorded) | 36% | 37% | 35% | 337 | Mixed | Paging through all versions |
| Litware | 13% | 22% | 37% | 71 | Litware (82%) | Paging through all versions |
| python-requests | 9.2% | 14% | 0% | 50 | Amazon (AWS) (98%) | Paging through latest versions |
| Go-http-client | 5.5% | 12% | 0% | 45 | Google (89%) | Paging through all versions |
| (no user agent) | 2.7% | 3.2% | 0% | 20 | Amazon (AWS) (58%) | Paging through latest versions |
| node | 7.3% | 2.0% | 97% | 58 | Microsoft (Azure) (82%) | Paging through latest versions |
| proseware-sync | 1.1% | 1.4% | 0% | 14 | Microsoft (Azure) (100%) | Paging through latest versions |
| litware | 0.6% | 1.4% | 0% | 4 | Litware (100%) | Paging through all versions |
| 64 other clients | 25% | 6.8% | 49% | 278 |  |  |

By network owner of the source address:

| Network | Requests | Data sent | Addresses seen |
| --- | --- | --- | --- |
| Amazon (AWS) | 27% | 33% | 87 |
| Litware | 17% | 28% | 84 |
| Google | 13% | 25% | 32 |
| Microsoft (Azure) | 13% | 5.3% | 32 |
| Acme Widgets Inc | 2.6% | 1.2% | 26 |
| Example Telecom Ltd | 5.1% | 1.0% | 32 |
| 38 other networks | 23% | 6.9% | 214 |

_Totals are from the gateway's request counters; shares are from 1,733 access records scaled to those totals. An organization is named from its user agent, or from the owner of its network when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._

**Caveats for this period:**

- Some records predate user-agent logging (Nov 12 09:10 UTC) and show as "(not recorded)".
