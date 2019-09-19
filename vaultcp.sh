#!/bin/bash

# A Vault secret migration code generator script.
# A source Vault is recursively listed and read and for each secret a write command will be generated to a worker script.
# A master script vaultcp.out will execute in parallel the worker scripts vaultcp.out.0 .. vaultcp.out.9
# The source vault is specified by VAULT_ADDR and an admnin token with VAULT_TOKEN env variables.
# The generated master script should have the destination Vault and token defined by different values for these env variables.

leaf() {
    local path=$1
    local elem=$2
    local fqp=$path$elem
    local dataelems=$(VAULT_TOKEN=$VAULT_TOKEN_SRC VAULT_ADDR=$VAULT_ADDR_SRC $VAULT_READ_CMD -field=data -format=json $fqp|jq -c .)

    let count=count+1
    mod=$(expr $count % 10)
    OUTPUTFILE=$OUT"."$mod
    if [[ ! -f $OUTPUTFILE ]]; then
        echo "#!/bin/bash" > $OUTPUTFILE
        echo "VAULT_TOKEN=$VAULT_TOKEN_DST" >> $OUTPUTFILE
        echo "VAULT_ADDR=$VAULT_ADDR_DST" >> $OUTPUTFILE
    fi
    echo "echo '$dataelems'|$VAULT_WRITE_CMD $fqp -" >> $OUTPUTFILE
}

list() {
    local path=$1
    local numelems=$(VAULT_TOKEN=$VAULT_TOKEN_SRC VAULT_ADDR=$VAULT_ADDR_SRC $VAULT_LIST_CMD -format=json $path|jq .|wc -l)

    let numelems2=numelems-2
    let lastindex=numelems2-1
    for i in $(seq 0 $lastindex); do
        elem=$(VAULT_TOKEN=$VAULT_TOKEN_SRC VAULT_ADDR=$VAULT_ADDR_SRC $VAULT_LIST_CMD -format=json $path|jq ".[$i]")

        elemsans=$(eval echo $elem) #strip surrounding quotes
        case $elemsans in
        */) list $path$elemsans;;
        *) leaf $path $elemsans;;
        esac
    done

}

main() {
    VAULT_TOKEN_SRC=$1
    VAULT_ADDR_SRC=$2
    VAULT_TOKEN_DST=$3
    VAULT_ADDR_DST=$4

    if [[ "$VAULT_TOKEN_SRC" = "" || "$VAULT_ADDR_SRC" = "" || "$VAULT_TOKEN_DST" = "" || "$VAULT_ADDR_DST" = "" ]]; then
        echo "Usage: vaultcp.sh <src token> <src addr> <dst token> <dst addr>"
        exit 1
    fi

    VER=$(vault version)
    echo $VER|grep "Vault v1" > /dev/null
    if [[ "$?" = "0" ]]; then
        VAULT_READ_CMD="vault kv get"
        VAULT_WRITE_CMD="vault kv put"
        VAULT_LIST_CMD="vault kv list"
    else
        VAULT_READ_CMD="vault read"
        VAULT_WRITE_CMD="vault write"
        VAULT_LIST_CMD="vault list"
    fi

    OUT=/tmp/vaultcp.out
    let count=0

    rm -f $OUT*

    echo "#!/bin/bash" > $OUT
    chmod +x $OUT

    list secret/
    echo "# There are $count secrets over 10 files" >> $OUT

    for i in $(seq 0 9); do
        OUTPUTFILE=$OUT"."$i
        echo "$OUTPUTFILE &" >> $OUT
    done
}

main $1 $2 $3 $4
