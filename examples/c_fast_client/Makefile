CC ?= clang
CFLAGS ?= -O3 -march=native -Wall -Wextra
INCLUDES := -I../../include
LDFLAGS := -L../../bin -laoni -Wl,-rpath,../../bin -lpthread

.PHONY: all clean run

all: client_example

client_example: main.c
	$(CC) $(CFLAGS) $(INCLUDES) main.c $(LDFLAGS) -o client_example

run: client_example
	./client_example

clean:
	rm -f client_example
