# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,073 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 1 distinct user agents.

- **Litware** made 16% of requests and received 26% of the data; – of its listing responses were compressed.
- 9 named organizations and products account for 28% of requests and 31% of the data. The rest cannot be tied to a name.
- Across all clients, – of listing responses were compressed, carrying – of listing data.

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
| Litware | Network owner | 16% | 26% | – | 82 |
| Acme Widgets Inc | Network owner | 3.4% | 3.7% | – | 30 |
| Tailspin Toys | Network owner | 0.6% | 0.4% | – | 6 |
| Lakeshore Dispatch | Network owner | 0.5% | 0.3% | – | 3 |
| Contoso Universite Reseau | Network owner | 0.8% | 0.2% | – | 6 |
| Example Savings Bank | Network owner | 2.6% | 0.2% | – | 31 |
| Fabrikam Hosting | Network owner | 3.5% | 0.2% | – | 18 |
| 2 other named |  | 0.7% | <0.1% | – | 32 |
| Unnamed clients on cloud hosting | Not identifiable | 54% | 64% | – | 178 |
| Unnamed clients on internet providers | Not identifiable | 18% | 4.9% | – | 136 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| (not recorded) | 100% | 100% | – | 522 | Mixed | Paging through latest versions |

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
