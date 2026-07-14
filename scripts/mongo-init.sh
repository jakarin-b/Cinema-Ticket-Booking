#!/bin/sh
set -eu

until mongosh --host mongodb:27017 --quiet --eval "db.adminCommand('ping').ok" >/dev/null 2>&1; do
  sleep 1
done

mongosh --host mongodb:27017 --quiet --eval '
try {
  const status = rs.status();
  if (status.ok !== 1) throw new Error("replica set is not ready");
} catch (error) {
  if (error.codeName === "NotYetInitialized" || String(error).includes("no replset config")) {
    rs.initiate({_id: "rs0", members: [{_id: 0, host: "mongodb:27017"}]});
  } else {
    throw error;
  }
}
'

until mongosh --host mongodb:27017 --quiet --eval "db.hello().isWritablePrimary" | grep true >/dev/null; do
  sleep 1
done

