PHP_ARG_WITH(gomesi, for gomesi library,
[  --with-gomesi=DIR        Path to libgomesi.so],
[  --with-gomesi=no])

if test "$PHP_GOMESI" != "no"; then
  if test -r "$PHP_GOMESI/libgomesi.so"; then
    MESI_LIBDIR="$PHP_GOMESI"
  else
    # Ewentualne przeszukanie innych ścieżek.
    SEARCH_PATH="/usr/local /usr"
    SEARCH_FOR="libgomesi.so"

    for i in $SEARCH_PATH ; do
      if test -r "$i/lib/$SEARCH_FOR"; then
        MESI_LIBDIR="$i/lib"
        break
      fi
    done
  fi

  if test -z "$MESI_LIBDIR"; then
    AC_MSG_ERROR([libgomesi.so not found. Specify path with --with-gomesi=DIR])
  fi

  PHP_ADD_LIBRARY_WITH_PATH(gomesi, $MESI_LIBDIR, MESI_SHARED_LIBADD)
  LDFLAGS="$LDFLAGS -lgomesi"
  PHP_SUBST(MESI_SHARED_LIBADD)
  PHP_NEW_EXTENSION(mesi, mesi.c, $ext_shared, $MESI_SHARED_LIBADD)
fi
