#include "libgomesi.h"
#include <stdio.h>
#include <stdlib.h>

char* read_file_contents(const char* filepath) {
    FILE* file = fopen(filepath, "r");
    if (!file) {
        perror("Error opening file");
        return NULL;
    }

    fseek(file, 0, SEEK_END);
    long file_size = ftell(file);
    rewind(file);

    char* buffer = malloc(file_size + 1);
    if (!buffer) {
        perror("Memory allocation error");
        fclose(file);
        return NULL;
    }

    size_t read_size = fread(buffer, 1, file_size, file);
    if (read_size != file_size) {
        perror("Error reading file");
        free(buffer);
        fclose(file);
        return NULL;
    }

    buffer[file_size] = '\0';
    fclose(file);
    return buffer;
}

int main(int argc, char *argv[]) {
    if (argc < 2) {
        printf("Usage: %s <input>\n", argv[0]);
        return 1;
    }

    char* file_contents = read_file_contents(argv[1]);
    if (!file_contents) {
        return 1;
    }

    char* result = Parse(file_contents);
    printf("Result: %s\n", result);

    FreeString(result);
    free(file_contents);
    return 0;
}