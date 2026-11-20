# Registry usage, Tue Oct 6, 2026 (UTC day)

**5,089 requests, 0.2 GiB sent** (about $0 at roughly $0.10 per GiB), from 1 distinct user agents.

- **Litware** made 17% of requests and received 28% of the data; – of its listing responses were compressed.
- 8 named organizations and products account for 27% of requests and 30% of the data. The rest cannot be tied to a name.
- Across all clients, – of listing responses were compressed, carrying – of listing data.

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
| Litware | Network owner | 17% | 28% | – | 84 |
| Acme Widgets Inc | Network owner | 2.3% | 1.0% | – | 22 |
| Tailspin Toys | Network owner | 0.8% | 0.6% | – | 6 |
| Fabrikam Hosting | Network owner | 2.6% | 0.2% | – | 17 |
| Example Savings Bank | Network owner | 2.7% | 0.2% | – | 32 |
| 3 other named |  | 1.2% | 0.1% | – | 34 |
| Unnamed clients on cloud hosting | Not identifiable | 56% | 66% | – | 173 |
| Unnamed clients on internet providers | Not identifiable | 17% | 3.9% | – | 139 |

By client software, as named in the user agent:

| Client | Requests | Data sent | Listings compressed | Addresses seen | Main network | Mostly does |
| --- | --- | --- | --- | --- | --- | --- |
| (not recorded) | 100% | 100% | – | 507 | Mixed | Paging through latest versions |

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
