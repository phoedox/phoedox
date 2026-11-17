# ```dbmodel – database schema as a diagram

SQL `CREATE TABLE` statements drawn as an entity-relationship diagram. `validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`.

**One database schema per project.** All `dbmodel` blocks of a project together form its
schema. A block draws its own `CREATE TABLE`s; a reference to a table defined on another
page is drawn too (reduced to its keys, with a link to that page). To show tables defined
elsewhere, list them in an SQL comment – no placeholder tables needed:

````markdown
```dbmodel
-- tables: users, sessions
CREATE TABLE devices (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));
```
````

**From a repository:** a block whose first line is `from owner/repo path` shows the
schema that the SQL migrations in that folder of a connected repository build
(project settings → Code & Git), in file order with `ALTER TABLE` and `DROP TABLE`
applied and down parts (goose, dbmate, `*.down.sql`) left out. It stays current
with the repository, so prefer it to copying `CREATE TABLE`s when the code lives
in a connected repository:

````markdown
```dbmodel
from acme/app db/migrations/postgres
```
````
