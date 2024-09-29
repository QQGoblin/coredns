# hook

## Name

*hook* - 启动`Coredns`时劫持本地`/etc/resolv.conf`

## Description

启动`Coredns`时劫持本地`/etc/resolv.conf`,在该文件中追加`nameserver 127.0.0.53`

## Syntax

~~~
hook [ResolvConf] [Nameserver1] [Nameserver2] [Nameserver2]
hook /etc/other-resolv.conf [Bind]
hook "" 127.0.0.53
hook /etc/other-resolv.conf 127.0.0.53
~~~