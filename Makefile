#
# Makefile
# veypi, 2025-08-06 01:42
#

version:
	@grep 'version = ' x.go|cut -f2 -d'"'

# 提交版本
tag:
	@awk -F '"' '/version/ {print $$2;system("git tag "$$2);system("git push origin "$$2)}' x.go

# 删除远端版本 慎用
dropTag:
	@awk -F '"' '/version/ {print $$2;system("git tag -d "$$2);system("git push origin :refs/tags/"$$2)}' x.go

