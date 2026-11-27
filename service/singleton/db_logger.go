package singleton

import (
 "time"
 "gorm.io/gorm/logger"
)

// Keep unexpected DB errors and slow-query diagnostics. ParameterizedQueries
// prevents GORM from interpolating bound credentials into SQL, including
// when DB.Debug() enables info-level tracing. It does not sanitize arbitrary
// raw SQL literals or driver-generated error text.
func databaseLogger(out logger.Writer)logger.Interface{
 return logger.New(out,logger.Config{
  SlowThreshold:200*time.Millisecond,
  LogLevel:logger.Warn,
  IgnoreRecordNotFoundError:true,
  ParameterizedQueries:true,
  Colorful:false,
 })
}
